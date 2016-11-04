package binexec

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
)

var (
	reapplyEvery = 5 * time.Minute
	binDir       = "/var/lib/cni/bin"
)

func Watch(c metadata.Client) error {
	w := &watcher{
		c:       c,
		applied: map[string]string{},
	}
	w.onChange("")
	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]string
	lastApplied time.Time
}

func (w *watcher) onChangeNoError(version string) {
	if err := w.onChange(version); err != nil {
		logrus.Errorf("Failed to apply cni conf: %v", err)
	}
}

func (w *watcher) onChange(version string) error {
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
			logrus.WithFields(logrus.Fields{
				"serviceKind":         service.Kind,
				"serviceName":         service.Name,
				"containerName":       container.Name,
				"containerExternalId": container.ExternalId,
				"containerHostUUID":   container.HostUUID,
				"driverLabel":         hasDriverLabel(container),
			}).Debugf("Checking for driver binary")
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

func (w *watcher) apply(host metadata.Host, binaries map[string]string) error {
	if !reflect.DeepEqual(binaries, w.applied) {
		logrus.Infof("Setting up binaries for: %v", binaries)
	}

	dockerVersion, ok := host.Labels["io.rancher.host.docker_version"]
	if !ok {
		logrus.Warnf("Failed to determine Docker version")
		dockerVersion = "unknown"
	}

	script := `#!/bin/bash
DOCKER_VERSION="%s"

if [ -e "$(which runc-$DOCKER_VERSION)" ]; then
	CMD=(runc-${DOCKER_VERSION})
else
	CMD=(runc)
fi


CMD+=("exec")
while read LINE; do
    CMD+=("-e")
    CMD+=("$LINE")
done < <(env)

exec "${CMD[@]}" %s %s "$@"
`

	os.MkdirAll(binDir, 0700)

	var lastErr error
	for name, target := range binaries {
		p := filepath.Join(binDir, name)
		content := []byte(fmt.Sprintf(script, dockerVersion, target, name))
		logrus.Debugf("Writing %s:\n%s", p, content)
		err := ioutil.WriteFile(p, content, 0700)
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}

func getBinaryName(container metadata.Container) string {
	return container.Labels["io.rancher.network.cni.binary"]
}

func hasDriverLabel(container metadata.Container) bool {
	return "" != container.Labels["io.rancher.network.cni.binary"]
}
