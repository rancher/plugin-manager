package vethsync

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/network"
	"github.com/vishvananda/netlink"
	"golang.org/x/net/context"
)

var (
	// DefaultSyncInterval specifies the default value
	// for vethsync interval in seconds
	DefaultSyncInterval = 60
)

// VethWatcher checks the conntrack table periodically for invalid
// entries and programs the appropriate ones if necessary based on info
// available from rancher-metadata
type VethWatcher struct {
	syncInterval time.Duration
	mc           metadata.Client
	dc           *client.Client
	lastApplied  time.Time
}

// Watch starts the go routine to periodically check the conntrack table
// for any discrepancies
func Watch(syncIntervalStr string, mc metadata.Client, dc *client.Client) error {
	logrus.Debugf("vethsync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i

	}

	vw := &VethWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
		dc:           dc,
	}

	go mc.OnChange(120, vw.onChangeNoError)

	return nil
}

func (vw *VethWatcher) onChangeNoError(version string) {
	logrus.Debugf("vethsync: metadata version: %v, lastApplied: %v", version, vw.lastApplied)
	timeSinceLastApplied := time.Now().Sub(vw.lastApplied)
	if timeSinceLastApplied < vw.syncInterval {
		timeToSleep := vw.syncInterval - timeSinceLastApplied
		logrus.Debugf("vethsync: sleeping for %v", timeToSleep)
		time.Sleep(timeToSleep)
	}
	if err := vw.doSync(); err != nil {
		logrus.Errorf("vethsync: while syncing, got error: %v", err)
	}
	vw.lastApplied = time.Now()
}

func (vw *VethWatcher) doSync() error {
	hostVethMap, err := vw.buildHostVethMap()
	if err != nil {
		logrus.Errorf("vethsync: error building hostVethMap list")
		return err
	}
	logrus.Debugf("vethsync: hostVethMap: %v", hostVethMap)

	containersVethMap, err := vw.buildContainersVethMap()
	if err != nil {
		logrus.Errorf("vethsync: error building containersVethMap")
		return err
	}
	logrus.Debugf("vethsync: containersVethMap: %v", containersVethMap)

	dangling, err := vw.checkForDanglingVeths(hostVethMap, containersVethMap)
	if err != nil {
		logrus.Errorf("vethsync: error checking for dangling veths: %v", err)
		return err
	}

	if len(dangling) > 0 {
		cleanUpDanglingVeths(dangling)
	}

	return nil
}

func cleanUpDanglingVeths(dangling map[string]*netlink.Link) error {
	logrus.Debugf("vethsync: cleaning up dangling veths")
	for _, v := range dangling {
		if err := netlink.LinkDel(*v); err != nil {
			logrus.Errorf("vethsync: error deleting dangling veth: %v", *v)
			continue
		}
	}
	return nil
}

func (vw *VethWatcher) checkForDanglingVeths(
	hostVethMap map[string]*netlink.Link, containerVethMap map[string]bool) (map[string]*netlink.Link, error) {
	logrus.Debugf("vethsync: checking for dangling veths")

	dangling := make(map[string]*netlink.Link)
	for k, v := range hostVethMap {
		_, found := containerVethMap[k]
		if !found {
			logrus.Debugf("vethsync: dangling veth found: %v", *v)
			dangling[k] = v
		}
	}

	logrus.Debugf("vethsync: dangling: %v", dangling)
	return dangling, nil
}

func (vw *VethWatcher) buildHostVethMap() (map[string]*netlink.Link, error) {
	// get docker bridge
	veths := make(map[string]*netlink.Link)

	alllinks, err := netlink.LinkList()
	if err != nil {
		logrus.Errorf("vethsync: error getting links: %v", err)
		return nil, err
	}

	localNetworks, _, err := network.LocalNetworks(vw.mc)
	if err != nil {
		logrus.Errorf("vethsync: error fetching local networks: %v", err)
		return nil, err
	}
	logrus.Debugf("vethsync: localNetworks: %v", localNetworks)

	localBridges := make(map[string]bool)
	for _, n := range localNetworks {
		cniConf, ok := n.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		b, err := getBridgeInfoFromCNIConfig(cniConf)
		if err != nil {
			continue
		}
		localBridges[b] = true
	}

	localBridgesLinksMap := make(map[int]*netlink.Link)
	for index, l := range alllinks {
		if _, found := localBridges[l.Attrs().Name]; found {
			localBridgesLinksMap[l.Attrs().Index] = &alllinks[index]
			logrus.Debugf("vethsync: found bridge link: %v", l)
		}
	}

	if len(localBridgesLinksMap) == 0 {
		err = fmt.Errorf("couldn't find any local bridge link")
		logrus.Errorf("vethsync: %v", err)
		return nil, err
	}
	logrus.Debugf("vethsync: localBridgesLinksMap: %v", localBridgesLinksMap)

	for index, l := range alllinks {
		if !strings.HasPrefix(l.Attrs().Name, "veth") {
			continue
		}
		if _, found := localBridgesLinksMap[l.Attrs().MasterIndex]; !found {
			continue
		}
		veths[strconv.Itoa(l.Attrs().Index)] = &alllinks[index]
	}

	return veths, nil
}

func getBridgeInfoFromCNIConfig(cniConf map[string]interface{}) (string, error) {
	var lastErr error
	var bridge string
	for _, config := range cniConf {
		props, ok := config.(map[string]interface{})
		if !ok {
			err := fmt.Errorf("error getting props from cni config")
			logrus.Errorf("vethsync: %v", err)
			lastErr = err
			continue
		}
		bridge, ok = props["bridge"].(string)
		if !ok {
			err := fmt.Errorf("error getting bridge from cni config")
			logrus.Errorf("vethsync: %v", err)
			lastErr = err
			continue
		}
	}

	logrus.Debugf("vethsync: bridge: %v", bridge)
	return bridge, lastErr
}

func (vw *VethWatcher) buildContainersVethMap() (map[string]bool, error) {
	containers, err := vw.dc.ContainerList(context.Background(), types.ContainerListOptions{})
	if err != nil {
		logrus.Errorf("vethsync: error fetching containers from docker client: %v", err)
		return nil, err
	}
	containerVethIndices := map[string]bool{}
	for _, aContainer := range containers {
		if aContainer.HostConfig.NetworkMode == "host" {
			continue
		}

		inspect, err := vw.dc.ContainerInspect(context.Background(), aContainer.ID)
		if err != nil {
			logrus.Errorf("vethsync: couldn't inspect container: %v", aContainer.ID)
			continue
		}

		if inspect.State == nil || inspect.State.Pid == 0 {
			continue
		}

		out, err := exec.Command("nsenter", "-t", strconv.Itoa(inspect.State.Pid), "-m", "-n", "cat", "/sys/class/net/eth0/iflink").Output()
		if err != nil {
			logrus.Errorf("vethsync: error running nsenter command: %v", err)
			return nil, err
		}
		index := strings.Replace(string(out), "\n", "", -1)
		containerVethIndices[index] = true
	}

	return containerVethIndices, nil
}
