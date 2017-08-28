package cniconf

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/cniglue"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/events"
	"github.com/rancher/plugin-manager/utils"
)

var (
	reapplyEvery = 5 * time.Minute
	cniDir       = "/etc/cni/%s.d"
)

func init() {
	glue.CniDir = cniDir
}

// Watch monitors metadata and generates CNI config
func Watch(c metadata.Client) error {
	w := &watcher{
		c:       c,
		applied: map[string]metadata.Network{},
	}
	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]metadata.Network
	lastApplied time.Time
}

func (w *watcher) onChangeNoError(version string) {
	if err := w.onChange(version); err != nil {
		logrus.Errorf("Failed to apply cni conf: %v", err)
	}
}

func (w *watcher) onChange(version string) error {
	networks, err := w.c.GetNetworks()
	if err != nil {
		return err
	}

	host, err := w.c.GetSelfHost()
	if err != nil {
		return err
	}

	forceApply := time.Now().Sub(w.lastApplied) > reapplyEvery

	for _, network := range networks {
		if network.EnvironmentUUID != host.EnvironmentUUID {
			logrus.Debugf("network: %v is not local to this environment", network.UUID)
			continue
		}
		_, ok := network.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		if forceApply || !reflect.DeepEqual(w.applied[network.Name], network) {
			if err := w.apply(network, host); err != nil {
				logrus.Errorf("Failed to apply cni conf: %v", err)
			}
		}
	}

	return nil
}

func (w *watcher) apply(network metadata.Network, host metadata.Host) error {
	cniConf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	confDir := fmt.Sprintf(cniDir, network.Name)
	if err := os.MkdirAll(confDir, 0700); err != nil {
		return err
	}

	var lastErr error
	for file, config := range cniConf {
		config = utils.UpdateCNIConfigByKeywords(config, host)

		checkMTU(config)

		p := filepath.Join(confDir, file)
		content, err := json.Marshal(config)
		if err != nil {
			lastErr = err
			continue
		}

		out := &bytes.Buffer{}
		if err := json.Indent(out, content, "", "  "); err != nil {
			lastErr = err
			continue
		}

		logrus.Debugf("Writing %s: %s", p, out)
		if err := ioutil.WriteFile(p, out.Bytes(), 0600); err != nil {
			lastErr = err
		}
	}

	if network.Default {
		managedDir := fmt.Sprintf(cniDir, "managed")
		managedDirTest, err := os.Stat(managedDir)
		configDirTest, err1 := os.Stat(confDir)
		if !(err == nil && err1 == nil && os.SameFile(managedDirTest, configDirTest)) {
			os.Remove(managedDir)
			if err := os.Symlink(network.Name+".d", managedDir); err != nil {
				lastErr = err
			}
		}
	}

	if lastErr == nil {
		w.applied[network.Name] = network
		w.lastApplied = time.Now()
	}

	return lastErr
}

func checkMTU(config interface{}) {
	props, _ := config.(map[string]interface{})
	bridgeName, _ := props["bridge"].(string)
	cniConfigMTU := fmt.Sprintf("%.0f", props["mtu"])

	dockerBridgeMTU, exist, err := getDockerNetworkBridgeMTU(bridgeName)
	if err != nil {
		logrus.Errorf("checkMTU: Got error from docker api: %s", err)
	}
	if exist && dockerBridgeMTU != cniConfigMTU {
		logrus.Errorf("checkMTU: Docker Bridge MTU %v is different from CNI config MTU %v", dockerBridgeMTU, cniConfigMTU)
	}
}

func getDockerNetworkBridgeMTU(bridgeName string) (string, bool, error) {
	c, err := events.NewDockerClient()
	if err != nil {
		return "", false, err
	}

	networks, err := c.ListNetworks()
	if err != nil {
		return "", false, err
	}

	for _, n := range networks {
		if n.Driver == "bridge" && n.Options["com.docker.network.bridge.name"] == bridgeName {
			return n.Options["com.docker.network.driver.mtu"], true, nil
		}
	}

	return "", false, nil
}
