package macsync

import (
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/log"
	"github.com/rancher/plugin-manager/network"
	"github.com/rancher/plugin-manager/utils"
	"github.com/vishvananda/netlink"
)

// MACSyncer ...
type MACSyncer struct {
	dc *client.Client
	mc metadata.Client
}

var (
	syncLabel    = "io.rancher.network.macsync"
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

	go ms.syncNTimes()
}

func (ms *MACSyncer) syncNTimes() {
	log.Debugf("macsync: start sync %v times", N)
	for {
		log.Debugf("macsync: syncing for first time")
		if done, err := ms.doSync(); err != nil {
			log.Errorf("macsync: error syncing MAC addresses for the first tiime: %v", err)
		} else if done {
			break
		}
		time.Sleep(syncInterval)
	}

	for i := 0; i < N; i++ {
		time.Sleep(syncInterval)
		log.Debugf("macsync: syncing i=%v time", i)
		if _, err := ms.doSync(); err != nil {
			log.Errorf("macsync: i: %v, error syncing MAC addresses: %v", i, err)
		}
	}
}

func (ms *MACSyncer) doSync() (bool, error) {
	didSomething := false

	networks, routers, err := utils.GetLocalNetworksAndRoutersFromMetadata(ms.mc)
	if err != nil {
		return didSomething, errors.Wrap(err, "getting local networks")
	}

	var lastError error
	for _, n := range networks {
		if routers[n.UUID].Labels[syncLabel] != "true" {
			continue
		}

		didSomething = true
		err := network.ForEachContainerNS(ms.dc, ms.mc, n.UUID, func(aContainer metadata.Container, _ ns.NetNS) error {
			log.Debugf("macsync: aContainer: %v", aContainer)
			l, err := netlink.LinkByName("eth0")
			if err != nil {
				log.Errorf("macsync: for container: %v, could not lookup interface: %v",
					aContainer, err)
				return err
			}
			foundMAC := l.Attrs().HardwareAddr.String()
			if !strings.EqualFold(aContainer.PrimaryMacAddress, foundMAC) {
				log.Infof("macsync: fixing container %v MAC address, found=%v, expected: %v",
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
	log.Debugf("macsync: didSomething=%v", didSomething)

	return didSomething, lastError
}
