package routesync

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/rancher/log"
	"github.com/vishvananda/netlink"
)

var (
	// DefaultSyncInterval specifies the default value for routesync interval
	DefaultSyncInterval = 60
)

// Watch makes sure the needed routes are programmed inside the container
func Watch(syncIntervalStr string) error {
	log.Debugf("routesync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i
	}

	//if conditions met, start the watcher
	conditionsMet, bridgeName, metadataIP := conditionsMetToWatch()

	if !conditionsMet {
		log.Debugf("routesync: conditions not met, hence not starting goroutine")
		return nil
	}

	// Add the route first before starting the goroutine so that
	// rest of the logic can do it's work
	if err := addRouteToMetadataIP(bridgeName, metadataIP); err != nil {
		return err
	}

	go doRouteSync(bridgeName, metadataIP, syncInterval)
	return nil
}

func doRouteSync(bridgeName, metadataIP string, syncInterval int) {
	log.Infof("routesync: starting monitoring on bridge: %v, for metadataIP: %v every %v seconds", bridgeName, metadataIP, syncInterval)
	for {
		time.Sleep(time.Duration(syncInterval) * time.Second)
		log.Debugf("routesync: time to sync routes")
		err := addRouteToMetadataIP(bridgeName, metadataIP)
		if err != nil {
			log.Errorf("routesync: while syncing routes, got error: %v", err)
		}
	}
}

// conditionsMetToWatch returns the status if the prerequisites
// are met to start the watcher
func conditionsMetToWatch() (bool, string, string) {
	dockerBridge := os.Getenv("DOCKER_BRIDGE")
	metadataIP := os.Getenv("METADATA_IP")

	log.Infof("routesync: DOCKER_BRIDGE=%v, METADATA_IP=%v", dockerBridge, metadataIP)

	if len(dockerBridge) > 0 && len(metadataIP) > 0 {
		return true, dockerBridge, metadataIP
	}
	return false, dockerBridge, metadataIP
}

func addRouteToMetadataIP(bridgeName, metadataIP string) error {
	log.Debugf("routesync: adding route to metadata IP address")

	l, err := netlink.LinkByName(bridgeName)
	if err != nil {
		return err
	}

	ip, err := netlink.ParseIPNet(metadataIP + "/32")
	if err != nil {
		return err
	}

	r := &netlink.Route{
		LinkIndex: l.Attrs().Index,
		Scope:     netlink.SCOPE_UNIVERSE,
		Dst:       ip,
	}

	err = netlink.RouteAdd(r)
	if err != nil {
		if err.Error() == "file exists" {
			mIP := net.ParseIP(metadataIP)
			existingRoutes, err := netlink.RouteGet(mIP)
			if err != nil {
				log.Errorf("routesync: error getting route: %v", err)
				return err
			}
			log.Debugf("routesync: existingRoutes: %#v", existingRoutes)
			if existingRoutes[0].LinkIndex != r.LinkIndex && existingRoutes[0].Dst != r.Dst {
				return fmt.Errorf("conflicting routes to metadata IP(%v): %v", metadataIP, existingRoutes)
			}
			log.Debugf("routesync: route already exisits, skipping")
		} else {
			log.Errorf("routesync: error adding route: %v", err)
			return err
		}
	} else {
		log.Infof("routesync: successfully added route to metadata IP(%v): %v", metadataIP, r)
	}

	return nil
}
