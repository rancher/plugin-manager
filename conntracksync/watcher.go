package conntracksync

import (
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/conntracksync/conntrack"
)

var (
	// DefaultSyncInterval specifies the default value
	// for conntracksync interval in seconds
	DefaultSyncInterval = 120
)

// ConntrackTableWatcher checks the conntrack table periodically for invalid
// entries and programs the appropriate ones if necessary based on info
// available from rancher-metadata
type ConntrackTableWatcher struct {
	syncInterval time.Duration
	mc           metadata.Client
}

// Watch starts the go routine to periodically check the ARP table
// for any discrepancies
func Watch(syncIntervalStr string, mc metadata.Client) error {
	logrus.Debugf("ctsync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i

	}

	ctw := &ConntrackTableWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
	}

	go ctw.syncLoop()

	return nil
}

func (ctw *ConntrackTableWatcher) syncLoop() {
	logrus.Infof("conntracksync: starting monitoring every %v seconds", ctw.syncInterval)
	for {
		time.Sleep(ctw.syncInterval)
		logrus.Debugf("conntracksync: time to sync ARP table")
		err := ctw.doSync()
		if err != nil {
			logrus.Errorf("conntracksync: while syncing, got error: %v", err)
		}
	}
}

func (ctw *ConntrackTableWatcher) doSync() error {
	logrus.Debugf("conntracksync: checking the conntrack table")

	containersMap, err := ctw.buildContainersMaps()
	if err != nil {
		logrus.Errorf("conntracksync: error building containersMap")
		return err
	}

	ctEntries, err := conntrack.ListDNAT()
	if err != nil {
		logrus.Errorf("conntracksync: error fetching conntrack entries")
		return err
	}

	for _, ctEntry := range ctEntries {
		key := ctEntry.OriginalDestinationPort + "/" + ctEntry.Protocol
		c, found := containersMap[key]
		if !found {
			continue
		}
		if ctEntry.ReplySourceIP != c.PrimaryIp {
			logrus.Infof("conntracksync: deleting mismatching conntrack entry found: %v. [expected: %v, got: %v]", ctEntry, c.PrimaryIp, ctEntry.ReplySourceIP)
			if err := conntrack.CTEntryDelete(ctEntry); err != nil {
				logrus.Errorf("conntracksync: error deleting the conntrack entry: %v", err)
			}
		}
	}

	return nil
}

func (ctw *ConntrackTableWatcher) buildContainersMaps() (
	map[string]*metadata.Container, error) {
	host, err := ctw.mc.GetSelfHost()
	if err != nil {
		logrus.Errorf("conntracksync: error fetching self host from metadata")
		return nil, err
	}

	containers, err := ctw.mc.GetContainers()
	if err != nil {
		logrus.Errorf("conntracksync: error fetching containers from metadata")
		return nil, err
	}
	containersMap := make(map[string]*metadata.Container)
	for index, aContainer := range containers {
		if !(aContainer.HostUUID == host.UUID && len(aContainer.Ports) > 0) {
			continue
		}

		for _, aPort := range aContainer.Ports {
			splits := strings.Split(aPort, ":")
			if len(splits) != 3 {
				continue
			}
			hostPort := splits[1]
			protocol := strings.Split(splits[2], "/")[1]

			containersMap[hostPort+"/"+protocol] = &containers[index]
		}
	}

	return containersMap, nil
}
