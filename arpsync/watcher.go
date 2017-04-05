package arpsync

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

var (
	// DefaultSyncInterval specifies the default value for arpsync interval in seconds
	DefaultSyncInterval = 5
)

// ARPTableWatcher checks the ARP table periodically for invalid entries
// and programs the appropriate ones if necessary based on info available
// from rancher-metadata
type ARPTableWatcher struct {
	syncInterval time.Duration
	mc           metadata.Client
	lastApplied  time.Time
}

// Watch starts the go routine to periodically check the ARP table
// for any discrepancies
func Watch(syncIntervalStr string, mc metadata.Client) error {
	logrus.Debugf("arpsync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i

	}

	atw := &ARPTableWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
	}

	go mc.OnChange(120, atw.onChangeNoError)

	return nil
}

func (atw *ARPTableWatcher) onChangeNoError(version string) {
	logrus.Debugf("arpsync: metadata version: %v, lastApplied: %v", version, atw.lastApplied)
	timeSinceLastApplied := time.Now().Sub(atw.lastApplied)
	if timeSinceLastApplied < atw.syncInterval {
		timeToSleep := atw.syncInterval - timeSinceLastApplied
		logrus.Debugf("arpsync: sleeping for %v", timeToSleep)
		time.Sleep(timeToSleep)
	}
	if err := atw.doSync(); err != nil {
		logrus.Errorf("arpsync: while syncing, got error: %v", err)
	}
	atw.lastApplied = time.Now()
}

func buildContainersMap(containers []metadata.Container,
	network metadata.Network) (map[string]*metadata.Container, error) {
	containersMap := make(map[string]*metadata.Container)

	for index, aContainer := range containers {
		if !(aContainer.PrimaryIp != "" &&
			aContainer.PrimaryMacAddress != "" &&
			aContainer.NetworkUUID == network.UUID) {
			continue
		}
		containersMap[aContainer.PrimaryIp] = &containers[index]
	}

	return containersMap, nil
}

func (atw *ARPTableWatcher) doSync() error {
	logrus.Debugf("arpsync: checking the ARP table %v", time.Now())
	networks, err := atw.mc.GetNetworks()
	if err != nil {
		logrus.Errorf("arpsync: error fetching networks from metadata")
		return err
	}

	host, err := atw.mc.GetSelfHost()
	if err != nil {
		logrus.Errorf("arpsync: error fetching self host from metadata")
		return err
	}

	services, err := atw.mc.GetServices()
	if err != nil {
		logrus.Errorf("arpsync: error fetching services from metadata")
		return err
	}

	var networkDriverMacAddress string
	localNetworks := map[string]bool{}
	for _, service := range services {
		// Trick to select the primary service of the network plugin
		// stack
		// TODO: Need to check if it's needed for Calico?
		if !(service.Kind == "networkDriverService" &&
			service.Name == service.PrimaryServiceName) {
			continue
		}

		for _, aContainer := range service.Containers {
			if aContainer.HostUUID == host.UUID {
				networkDriverMacAddress = aContainer.PrimaryMacAddress
				localNetworks[aContainer.NetworkUUID] = true
			}
		}
	}
	if len(localNetworks) == 0 {
		return fmt.Errorf("couldn't find any local networks")
	}
	logrus.Debugf("arpsync: localNetworks: %v", localNetworks)
	logrus.Debugf("arpsync: networkDriverMacAddress=%v", networkDriverMacAddress)

	var localNetwork metadata.Network
	for _, aNetwork := range networks {
		if _, ok := localNetworks[aNetwork.UUID]; ok {
			localNetwork = aNetwork
			break
		}
	}
	logrus.Debugf("arpsync: localNetwork: %+v", localNetwork)

	// Read the ARP table
	entries, err := netlink.NeighList(0, netlink.FAMILY_V4)
	if err != nil {
		logrus.Errorf("arpsync: error fetching entries from ARP table")
		return err
	}
	logrus.Debugf("arpsync: entries=%+v", entries)

	containers, err := atw.mc.GetContainers()
	if err != nil {
		logrus.Errorf("arpsync: error fetching containers from metadata")
		return err
	}
	containersMap, err := buildContainersMap(containers, localNetwork)

	for _, aEntry := range entries {
		if container, found := containersMap[aEntry.IP.String()]; found {
			if container.HostUUID == host.UUID {
				if container.PrimaryMacAddress != aEntry.HardwareAddr.String() {
					logrus.Infof("arpsync: wrong ARP entry found=%+v(expected: %v) for local container, fixing it", aEntry, container.PrimaryMacAddress)
					fixARPEntry(aEntry, container.PrimaryMacAddress)
				}
			} else {
				if aEntry.HardwareAddr.String() != networkDriverMacAddress {
					logrus.Errorf("arpsync: wrong ARP entry found=%+v(expected: %v) for remote container, fixing it", aEntry, networkDriverMacAddress)
					fixARPEntry(aEntry, networkDriverMacAddress)
				}
			}
		} else {
			logrus.Debugf("arpsync: container not found for ARP entry: %+v", aEntry)
		}
	}

	return nil
}

func fixARPEntry(oldEntry netlink.Neigh, newMACAddress string) error {
	var err error
	var newHardwareAddr net.HardwareAddr
	if newHardwareAddr, err = net.ParseMAC(newMACAddress); err != nil {
		logrus.Errorf("arpsync: couldn't parse MAC address(%v): %v", newMACAddress, err)
		return err
	}
	newEntry := oldEntry
	newEntry.HardwareAddr = newHardwareAddr
	newEntry.Type = netlink.NUD_REACHABLE
	if err = netlink.NeighSet(&newEntry); err != nil {
		logrus.Errorf("arpsync: error changing ARP entry: %v", err)
		return err
	}
	return nil
}
