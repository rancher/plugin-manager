package conntracksync

import (
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/conntracksync/conntrack"
	"github.com/rancher/plugin-manager/utils"
)

var (
	// DefaultSyncInterval specifies the default value
	// for conntracksync interval in seconds
	DefaultSyncInterval = 5
)

// ConntrackTableWatcher checks the conntrack table periodically for invalid
// entries and programs the appropriate ones if necessary based on info
// available from rancher-metadata
type ConntrackTableWatcher struct {
	syncInterval time.Duration
	mc           metadata.Client
	lastApplied  time.Time
}

// Watch starts the go routine to periodically check the conntrack table
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

	go mc.OnChange(120, ctw.onChangeNoError)

	return nil
}

func (ctw *ConntrackTableWatcher) onChangeNoError(version string) {
	logrus.Debugf("ctsync: metadata version: %v, lastApplied: %v", version, ctw.lastApplied)
	timeSinceLastApplied := time.Now().Sub(ctw.lastApplied)
	if timeSinceLastApplied < ctw.syncInterval {
		timeToSleep := ctw.syncInterval - timeSinceLastApplied
		logrus.Debugf("ctsync: sleeping for %v", timeToSleep)
		time.Sleep(timeToSleep)
	}
	if err := ctw.doSync(); err != nil {
		logrus.Errorf("ctsync: while syncing, got error: %v", err)
	}
	ctw.lastApplied = time.Now()
}

func (ctw *ConntrackTableWatcher) doSync() error {
	containersMap, err := ctw.buildContainersMaps()
	if err != nil {
		logrus.Errorf("conntracksync: error building containersMap")
		return err
	}

	dCTEntries, err := conntrack.ListDNAT()
	if err != nil {
		logrus.Errorf("conntracksync: error fetching DNAT conntrack entries")
		return err
	}

	for _, ctEntry := range dCTEntries {
		var c *metadata.Container
		var specificEntryFound, genericEntryFound bool
		specificKey := ctEntry.OriginalDestinationIP + ":" + ctEntry.OriginalDestinationPort + "/" + ctEntry.Protocol
		c, specificEntryFound = containersMap[specificKey]
		if !specificEntryFound {
			genericKey := "0.0.0.0:" + ctEntry.OriginalDestinationPort + "/" + ctEntry.Protocol
			c, genericEntryFound = containersMap[genericKey]
			if !genericEntryFound {
				continue
			}
		}
		if c.PrimaryIp != "" && ctEntry.ReplySourceIP != c.PrimaryIp {
			logrus.Infof("conntracksync: deleting mismatching DNAT conntrack entry found: %v. [expected: %v, got: %v]", ctEntry, c.PrimaryIp, ctEntry.ReplySourceIP)
			if err := conntrack.CTEntryDelete(ctEntry); err != nil {
				logrus.Errorf("conntracksync: error deleting the conntrack entry: %v", err)
			}
		}
	}

	sCTEntries, err := conntrack.ListSNAT()
	if err != nil {
		logrus.Errorf("conntracksync: error fetching SNAT conntrack entries")
		return err
	}

	for _, ctEntry := range sCTEntries {
		var c *metadata.Container
		var specificEntryFound, genericEntryFound bool
		specificKey := ctEntry.ReplyDestinationIP + ":" + ctEntry.ReplyDestinationPort + "/" + ctEntry.Protocol
		c, specificEntryFound = containersMap[specificKey]
		if !specificEntryFound {
			genericKey := "0.0.0.0:" + ctEntry.ReplyDestinationPort + "/" + ctEntry.Protocol
			c, genericEntryFound = containersMap[genericKey]
			if !genericEntryFound {
				continue
			}
		}
		if c.PrimaryIp != "" && ctEntry.OriginalSourceIP != c.PrimaryIp {
			logrus.Infof("conntracksync: deleting mismatching SNAT conntrack entry found: %v. [expected: %v, got: %v]", ctEntry, c.PrimaryIp, ctEntry.OriginalSourceIP)
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
		if !(aContainer.HostUUID == host.UUID &&
			utils.IsContainerConsideredRunning(aContainer) &&
			len(aContainer.Ports) > 0) {
			continue
		}

		for _, aPort := range aContainer.Ports {
			protocol := "tcp"
			splits := strings.Split(aPort, ":")
			if len(splits) != 3 {
				continue
			}
			hostIP := splits[0]
			hostPort := splits[1]
			targetPort := splits[2]
			parts := strings.Split(targetPort, "/")
			if len(parts) == 2 {
				protocol = parts[1]
			}

			containersMap[hostIP+":"+hostPort+"/"+protocol] = &containers[index]
		}
	}

	return containersMap, nil
}
