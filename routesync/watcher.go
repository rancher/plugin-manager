package routesync

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/vishvananda/netlink"
)

var (
	// DefaultSyncInterval specifies the default value for routesync interval
	DefaultSyncInterval = 60
)

// Watch makes sure the needed routes are programmed inside the container
func Watch(syncIntervalStr string) error {
	logrus.Debugf("routesynce: syncIntervalStr: %v", syncIntervalStr)

	var syncInterval int
	if syncIntervalStr != "" {
		i, err := strconv.Atoi(syncIntervalStr)
		if err != nil {
			syncInterval = DefaultSyncInterval
		} else {
			syncInterval = i
		}
	} else {
		syncInterval = DefaultSyncInterval
	}

	//if conditions met, start the watcher
	conditionsMet, bridgeName, metadataIP := conditionsMetToWatch()

	if conditionsMet {
		err := addRouteToMetadataIP(bridgeName, metadataIP)
		if err != nil {
			return err
		}
		go doRouteSync(bridgeName, metadataIP, syncInterval)
	}
	return nil
}

func doRouteSync(bridgeName, metadataIP string, syncInterval int) {
	for {
		time.Sleep(time.Duration(syncInterval) * time.Second)
		logrus.Debugf("routesync: time to sync routes")
		err := addRouteToMetadataIP(bridgeName, metadataIP)
		if err != nil {
			logrus.Errorf("routesynce: while syncing routes, got error: %v", err)
		}
	}
}

// conditionsMetToWatch returns the status if the prerequisites
// are met to start the watcher
func conditionsMetToWatch() (bool, string, string) {
	dockerBridge := os.Getenv("DOCKER_BRIDGE")
	metadataIP := os.Getenv("METADATA_IP")

	logrus.Infof("routesync: DOCKER_BRIDGE=%v, METADATA_IP=%v", dockerBridge, metadataIP)

	if len(dockerBridge) > 0 && len(metadataIP) > 0 {
		return true, dockerBridge, metadataIP
	}
	return false, dockerBridge, metadataIP
}

func addRouteToMetadataIP(bridgeName, metadataIP string) error {
	logrus.Debugf("routesynce: adding route to metadata IP address")

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
				logrus.Errorf("routesync: error getting route: %v", err)
				return err
			}
			logrus.Debugf("routesync: existingRoutes: %#v", existingRoutes)
			if existingRoutes[0].LinkIndex != r.LinkIndex && existingRoutes[0].Dst != r.Dst {
				return fmt.Errorf("conflicting routes to metadata IP(%v): %v", metadataIP, existingRoutes)
			}
			logrus.Debugf("routesync: route already exisits, skipping")
		} else {
			logrus.Errorf("routesync: error adding route: %v", err)
			return err
		}
	} else {
		logrus.Infof("routesync: successfully added route to metadata IP(%v): %v", metadataIP, r)
	}

	return nil
}
