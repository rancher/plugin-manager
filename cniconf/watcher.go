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
	"github.com/rancher/go-rancher-metadata/metadata"
)

var (
	reapplyEvery = 5 * time.Minute
	cniDir       = "/etc/docker/cni/%s.d"
)

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

	forceApply := time.Now().Sub(w.lastApplied) > reapplyEvery

	for _, network := range networks {
		_, ok := network.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		if forceApply || !reflect.DeepEqual(w.applied[network.Name], network) {
			if err := w.apply(network); err != nil {
				logrus.Errorf("Failed to apply cni conf: %v", err)
			}
		}
	}

	return nil
}

func (w *watcher) apply(network metadata.Network) error {
	cniConf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	confDir := fmt.Sprintf(cniDir, network.Name)
	if err := os.MkdirAll(confDir, 0700); err != nil {
		return err
	}

	var lastErr error
	for file, config := range cniConf {
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
		defaultDir := fmt.Sprintf(cniDir, "default")
		defaultDirTest, err := os.Stat(defaultDir)
		configDirTest, err1 := os.Stat(confDir)
		if !(err == nil && err1 == nil && os.SameFile(defaultDirTest, configDirTest)) {
			os.Remove(defaultDir)
			if err := os.Symlink(network.Name+".d", defaultDir); err != nil {
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
