package binexec

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"time"

	"github.com/docker/engine-api/client"
	docker "github.com/fsouza/go-dockerclient"
	"github.com/rancher/cniglue"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/log"
)

var (
	reapplyEvery = 5 * time.Minute
	binDir       = glue.CniPath[0]
)

func Watch(c metadata.Client, dc *client.Client) *Watcher {
	w := &Watcher{
		c:       c,
		dc:      dc,
		applied: map[string]string{},
	}
	w.onChange("")
	go c.OnChange(5, w.onChangeNoError)
	return w
}

type Watcher struct {
	sync.Mutex
	c           metadata.Client
	dc          *client.Client
	applied     map[string]string
	lastApplied time.Time
}

func (w *Watcher) onChangeNoError(version string) {
	if err := w.onChange(version); err != nil {
		log.Errorf("Failed to apply cni conf: %v", err)
	}
}

func (w *Watcher) Handle(event *docker.APIEvents) error {
	w.Lock()

	changed := false
	for _, v := range w.applied {
		if v == event.ID {
			changed = true
			break
		}
	}

	w.lastApplied = time.Time{}
	w.Unlock()

	if changed {
		return w.onChange("")
	}
	return nil
}

func (w *Watcher) onChange(version string) error {
	w.Lock()
	defer w.Unlock()

	binaries := map[string]string{}
	driverServices := map[string]metadata.Service{}

	services, err := w.c.GetServices()
	if err != nil {
		return err
	}

	host, err := w.c.GetSelfHost()
	if err != nil {
		return err
	}

	for _, service := range services {
		if service.Kind != "networkDriverService" && service.Kind != "storageDriverService" {
			continue
		}

		driverServices[service.StackUUID+"/"+service.Name] = service
	}

	for _, service := range services {
		if _, ok := driverServices[service.StackUUID+"/"+service.PrimaryServiceName]; ok {
			driverServices[service.StackUUID+"/"+service.Name] = service
		}
	}

	for _, service := range driverServices {
		for _, container := range service.Containers {
			log.Debugf("Checking for driver binary: driverLabel=%v containerHostUUID=%v containerExternalId=%v containerName=%v serviceName=%v serviceKind=%v",
				hasDriverLabel(container), container.HostUUID, container.ExternalId, container.Name, service.Name, service.Kind)

			if container.ExternalId != "" && container.HostUUID == host.UUID && hasDriverLabel(container) {
				binName := getBinaryName(container)
				if binName != "" {
					binaries[binName] = container.ExternalId
				}
			}
		}
	}

	if time.Now().Sub(w.lastApplied) > reapplyEvery || !reflect.DeepEqual(binaries, w.applied) {
		return w.apply(host, binaries)
	}

	return nil
}

func (w *Watcher) apply(host metadata.Host, binaries map[string]string) error {
	if !reflect.DeepEqual(binaries, w.applied) {
		log.Infof("Setting up binaries for: %v", binaries)
	}

	script := `#!/bin/sh
exec /usr/bin/nsenter -m -u -i -n -p -t %d -- $0 "$@"
`

	os.MkdirAll(binDir, 0700)

	var lastErr error
	for name, target := range binaries {
		container, err := w.dc.ContainerInspect(context.Background(), target)
		if err != nil {
			lastErr = err
			break
		}

		if container.State == nil || container.State.Pid == 0 {
			lastErr = fmt.Errorf("container is not running")
			break
		}

		ptmp := filepath.Join(binDir, name+".tmp")
		p := filepath.Join(binDir, name)
		content := []byte(fmt.Sprintf(script, container.State.Pid))
		log.Debugf("Writing %s:\n%s", p, content)
		if err := ioutil.WriteFile(ptmp, content, 0700); err != nil {
			lastErr = err
			break
		}

		fileInfo, err := os.Stat(p)
		if err == nil && fileInfo.IsDir() {
			log.Infof("%s is a dir, remove it", p)
			if err = os.Remove(p); err != nil {
				lastErr = err
				break
			}
		}

		if err := os.Rename(ptmp, p); err != nil {
			lastErr = err
		}
	}

	if lastErr == nil {
		w.applied = binaries
		w.lastApplied = time.Now()
	}

	return lastErr
}

func getBinaryName(container metadata.Container) string {
	return container.Labels["io.rancher.network.cni.binary"]
}

func hasDriverLabel(container metadata.Container) bool {
	return "" != container.Labels["io.rancher.network.cni.binary"]
}
