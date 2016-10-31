package cniconf

import (
	"bytes"
	"encoding/json"
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
	confDir      = "/tmp/foo"
)

func Watch(c metadata.Client) error {
	w := &watcher{
		c:       c,
		applied: map[string]map[string]interface{}{},
	}
	go c.OnChange(5, w.onChangeNoError)
	return nil
}

type watcher struct {
	c           metadata.Client
	applied     map[string]map[string]interface{}
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
		conf, ok := network.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		if forceApply || !reflect.DeepEqual(w.applied[network.Name], conf) {
			w.apply(network.Name, conf)
		}
	}

	return nil
}

func (w *watcher) apply(network string, cniConf map[string]interface{}) error {
	if err := os.MkdirAll(filepath.Join(confDir, network), 0700); err != nil {
		return err
	}

	var lastErr error
	for file, config := range cniConf {
		p := filepath.Join(confDir, network, file)
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

	if lastErr == nil {
		w.applied[network] = cniConf
		w.lastApplied = time.Now()
	}

	return lastErr
}
