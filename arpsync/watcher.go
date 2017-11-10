package arpsync

import (
	"net"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/network"
	"github.com/rancher/plugin-manager/utils"
	"github.com/vishvananda/netlink"
)

var (
	// DefaultSyncInterval specifies the default value for arpsync interval in seconds
	DefaultSyncInterval = 5
	syncLabel           = "io.rancher.network.arpsync"
)

// ARPTableWatcher checks the ARP table periodically for invalid entries
// and programs the appropriate ones if necessary based on info available
// from rancher-metadata
type ARPTableWatcher struct {
	syncInterval     time.Duration
	mc               metadata.Client
	dc               *client.Client
	knownRouters     map[string]metadata.Container
	routerApplyTries int
	lastApplied      time.Time
}

// Watch starts the go routine to periodically check the ARP table
// for any discrepancies
func Watch(syncIntervalStr string, mc metadata.Client, dc *client.Client) error {
	logrus.Debugf("arpsync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i

	}

	atw := &ARPTableWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
		dc:           dc,
		knownRouters: map[string]metadata.Container{},
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
			utils.IsContainerConsideredRunning(aContainer) &&
			aContainer.NetworkUUID == network.UUID) {
			continue
		}
		containersMap[aContainer.PrimaryIp] = &containers[index]
	}

	return containersMap, nil
}

func (atw *ARPTableWatcher) doSync() error {
	host, err := atw.mc.GetSelfHost()
	if err != nil {
		return errors.Wrap(err, "get self host")
	}

	containers, err := atw.mc.GetContainers()
	if err != nil {
		return errors.Wrap(err, "error fetching containers from metadata")
	}

	var lastError error
	localNetworks, routers, err := utils.GetLocalNetworksAndRoutersFromMetadata(atw.mc)
	if err != nil {
		return errors.Wrap(err, "get local networks")
	}

	logrus.Debugf("arpsync: atw.knownRouters=%v", atw.knownRouters)

	for _, localNetwork := range localNetworks {
		if routers[localNetwork.UUID].Labels[syncLabel] != "true" {
			continue
		}

		containersMap, err := buildContainersMap(containers, localNetwork)
		if err != nil {
			return errors.Wrap(err, "building containers map")
		}

		networkDriverMacAddress := routers[localNetwork.UUID].PrimaryMacAddress
		if networkDriverMacAddress == "" {
			continue
		}

		err = syncArpTable("host", networkDriverMacAddress, containersMap, host)
		if err != nil {
			lastError = err
		}

		if atw.knownRouters[localNetwork.UUID].PrimaryMacAddress != networkDriverMacAddress || atw.routerApplyTries < 10 {
			if atw.knownRouters[localNetwork.UUID].PrimaryMacAddress != networkDriverMacAddress {
				logrus.Debugf("arpsync: network router mac address changed from=%v to=%v", atw.knownRouters[localNetwork.UUID].PrimaryMacAddress, networkDriverMacAddress)
				atw.routerApplyTries = 0
			}

			atw.routerApplyTries++
			logrus.Infof("arpsync: Network router changed, syncing ARP tables %d/10 in containers, new MAC: %v", atw.routerApplyTries, networkDriverMacAddress)
			err := network.ForEachContainerNS(atw.dc, atw.mc, localNetwork.UUID, func(container metadata.Container, _ ns.NetNS) error {
				return syncArpTable(container.ExternalId, networkDriverMacAddress, containersMap, host)
			})
			if err != nil {
				logrus.Errorf("arpsync: got error while syncing arp tables for containers=%v", err)
				lastError = err
			}
		}
	}

	if lastError == nil {
		atw.knownRouters = routers
	}

	return lastError
}

func syncArpTable(context string, networkDriverMacAddress string, containersMap map[string]*metadata.Container, host metadata.Host) error {
	linkIndex := 0
	if context != "host" {
		link, err := netlink.LinkByName("eth0")
		if err != nil {
			logrus.Errorf("arpsync): error fetching eth0 link for %v: %v", context, err)
			return err
		}
		linkIndex = link.Attrs().Index
	}
	// Read the ARP table
	entries, err := netlink.NeighList(linkIndex, netlink.FAMILY_V4)
	if err != nil {
		logrus.Errorf("arpsync(%v): error fetching entries from ARP table", context)
		return err
	}
	logrus.Debugf("arpsync(%v): entries=%+v", context, entries)

	for _, aEntry := range entries {
		if container, found := containersMap[aEntry.IP.String()]; found {
			expected := networkDriverMacAddress
			if container.HostUUID == host.UUID {
				expected = container.PrimaryMacAddress
			}

			if aEntry.HardwareAddr.String() != expected {
				logrus.Infof("arpsync: (%s) wrong ARP entry found=%+v(expected: %v) for local container, fixing it", context, aEntry, expected)
				fixARPEntry(aEntry, expected)
			}
		} else {
			logrus.Debugf("arpsync(%v): container not found for ARP entry: %+v", context, aEntry)
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
	newEntry.State = netlink.NUD_REACHABLE
	if err = netlink.NeighSet(&newEntry); err != nil {
		logrus.Errorf("arpsync: error changing ARP entry: %v", err)
		return err
	}
	return nil
}
