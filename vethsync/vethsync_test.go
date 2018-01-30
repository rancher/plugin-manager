package vethsync

import (
	"testing"

	"github.com/docker/engine-api/client"
	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// Some of the tests can run only when in development,
// remember to disable this before commiting the code.
const inDevelopment = false

func getTestVethWatcher() (*VethWatcher, error) {
	metadataURL := "http://169.254.169.250/2016-07-29"
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		log.Errorf("error creating metadata client")
		return nil, err
	}
	dClient, err := client.NewEnvClient()
	if err != nil {
		log.Errorf("err=%v", err)
		return nil, err
	}
	return &VethWatcher{
		mc:          mc,
		metadataURL: metadataURL,
		dc:          dClient,
		debug:       true,
	}, nil
}

func TestDoSync(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("debug")
	log.Debugf("TestDoSync")

	vw, err := getTestVethWatcher()
	if err != nil {
		t.Fatalf("not expecting error: %v", err)
	}
	if err := vw.doSync(); err != nil {
		log.Errorf("vethsync: while syncing on startup, got error: %v", err)
		t.Fatalf("not expecting error: %v", err)
	}
}
