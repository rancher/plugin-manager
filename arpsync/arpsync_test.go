package arpsync

import (
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// Some of the tests can run only when in development,
// remember to disable this before commiting the code.
const inDevelopment = false

func TestDoSync(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)
	logrus.Debugf("TestDoSync")
	mc, err := metadata.NewClientAndWait("http://169.254.169.250/2016-07-29")
	if err != nil {
		logrus.Errorf("error creating metadata client")
		t.Fail()
	}

	atw := &ARPTableWatcher{
		syncInterval: time.Duration(10) * time.Second,
		mc:           mc,
	}

	if err := atw.doSync(); err != nil {
		logrus.Errorf("arpsync: error doing a sync of the ARP table: %v", err)
	}
}

// For development purposes
func TestArpWatcher(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)
	logrus.Debugf("TestArpWatcher")
	mc, err := metadata.NewClientAndWait("http://169.254.169.250/2016-07-29")
	if err != nil {
		logrus.Errorf("error creating metadata client")
		t.Fail()
	}

	atw := &ARPTableWatcher{
		syncInterval: time.Duration(10) * time.Second,
		mc:           mc,
	}

	atw.syncLoop()
}
