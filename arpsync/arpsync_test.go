package arpsync

import (
	"testing"
	"time"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// Some of the tests can run only when in development,
// remember to disable this before commiting the code.
const inDevelopment = false

func TestDoSync(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	log.SetLevelString("string")
	log.Debugf("TestDoSync")
	mc, err := metadata.NewClientAndWait("http://169.254.169.250/2016-07-29")
	if err != nil {
		log.Errorf("error creating metadata client")
		t.Fail()
	}

	atw := &ARPTableWatcher{
		syncInterval: time.Duration(10) * time.Second,
		mc:           mc,
	}

	if err := atw.doSync(); err != nil {
		log.Errorf("arpsync: error doing a sync of the ARP table: %v", err)
	}
}
