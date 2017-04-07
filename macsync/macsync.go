package macsync

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/network"
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
	networks, _, err := network.LocalNetworks(ms.mc)
	if err != nil {
		return errors.Wrap(err, "getting local networks")
	}

	var lastError error
	for _, n := range networks {
		err := network.ForEachContainerNS(ms.dc, ms.mc, n.UUID, func(aContainer metadata.Container, _ ns.NetNS) error {
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
		})
		if err != nil {
			lastError = err
		}
	}

	return lastError
}
