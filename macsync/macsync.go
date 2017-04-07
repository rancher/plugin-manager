package macsync

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/vishvananda/netlink"
)

// MACSyncer ...
type MACSyncer struct {
	dc *client.Client
	mc metadata.Client
}

var (
	syncInterval = 15 * time.Second
	// N for 2 min
	N = 8
)

// SyncMACAddresses syncs the MAC addresses of all the running
// containers on the host with info from metadata. This is
// especillay needed during an upgrade when the MAC of the
// container was not set during a prior release
func SyncMACAddresses(mc metadata.Client, dockerClient *client.Client) {
	ms := MACSyncer{
		mc: mc,
		dc: dockerClient,
	}

	if err := ms.doSync(); err != nil {
		logrus.Errorf("macsync: error syncing MAC addresses for the first tiime: %v", err)
	}

	go ms.syncNTimes()
}

func (ms *MACSyncer) syncNTimes() {
	for i := 0; i < N; i++ {
		time.Sleep(syncInterval)
		if err := ms.doSync(); err != nil {
			logrus.Errorf("macsync: i: %v, error syncing MAC addresses: %v", i, err)
		}
	}
}

func (ms *MACSyncer) doSync() error {
	networks, err := ms.mc.GetNetworks()
	if err != nil {
		logrus.Errorf("macsync: error fetching networks from metadata")
		return err
	}

	host, err := ms.mc.GetSelfHost()
	if err != nil {
		logrus.Errorf("macsync: error fetching self host from metadata")
		return err
	}

	containers, err := ms.mc.GetContainers()
	if err != nil {
		logrus.Errorf("macsync: error fetching containers from metadata")
		return err
	}

	services, err := ms.mc.GetServices()
	if err != nil {
		logrus.Errorf("macsync: error fetching services from metadata")
		return err
	}

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
				localNetworks[aContainer.NetworkUUID] = true
			}
		}
	}
	if len(localNetworks) == 0 {
		return fmt.Errorf("couldn't find any local networks")
	}
	logrus.Debugf("macsync: localNetworks: %v", localNetworks)

	var localNetwork metadata.Network
	for _, aNetwork := range networks {
		if _, ok := localNetworks[aNetwork.UUID]; ok {
			localNetwork = aNetwork
			break
		}
	}
	logrus.Debugf("macsync: localNetwork: %+v", localNetwork)

	for _, aContainer := range containers {
		if !(aContainer.HostUUID == host.UUID &&
			aContainer.State == "running" &&
			aContainer.ExternalId != "" &&
			aContainer.PrimaryMacAddress != "" &&
			aContainer.NetworkUUID == localNetwork.UUID) {
			continue
		}

		inspect, err := ms.dc.ContainerInspect(context.Background(), aContainer.ExternalId)
		if err != nil {
			logrus.Errorf("macsync: error inspecting container: %v", aContainer.ExternalId)
			continue
		}

		containerNSStr := fmt.Sprintf("/proc/%v/ns/net", inspect.State.Pid)
		netns, err := ns.GetNS(containerNSStr)
		if err != nil {
			logrus.Errorf("macsync: failed to open netns %v: %v", containerNSStr, err)
			continue
		}
		defer netns.Close()

		if err := netns.Do(func(_ ns.NetNS) error {
			l, err := netlink.LinkByName("eth0")
			if err != nil {
				logrus.Errorf("macsync: for container: %v, could not lookup interface: %v",
					aContainer, err)
				return err
			}
			foundMAC := l.Attrs().HardwareAddr.String()
			if !strings.EqualFold(aContainer.PrimaryMacAddress, foundMAC) {
				logrus.Infof("macsync: fixing container %v MAC address, found=%v, expected: %v",
					aContainer.ExternalId, foundMAC, aContainer.PrimaryMacAddress)

				hwaddr, err := net.ParseMAC(aContainer.PrimaryMacAddress)
				if err != nil {
					return fmt.Errorf("failed to parse MAC address: %v", err)
				}
				err = netlink.LinkSetHardwareAddr(l, hwaddr)
				if err != nil {
					return fmt.Errorf("failed to set hw address of interface: %v", err)
				}
			}
			return nil
		}); err != nil {
			logrus.Errorf("macsync: err=%v", err)
			continue
		}
	}
	return nil
}
