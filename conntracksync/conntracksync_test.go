package conntracksync

import (
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
)

func TestDoSync(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.Debugf("TestDoSync")
	mc, err := metadata.NewClientAndWait("http://169.254.169.250/2016-07-29")
	if err != nil {
		logrus.Errorf("error creating metadata client")
		t.Fail()
	}

	ctw := &ConntrackTableWatcher{
		syncInterval: time.Duration(10) * time.Second,
		mc:           mc,
	}

	if err := ctw.doSync(); err != nil {
		logrus.Errorf("conntracksync: error doing a sync of the conntrack table: %v", err)
	}
}
