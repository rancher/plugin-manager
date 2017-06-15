package vethsync

import (
	"os"
	"os/exec"
	"strconv"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/vethsync/utils"
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
	metadataURL  string
	mc           metadata.Client
	dc           *client.Client
	debug        bool
	lastApplied  time.Time
}

// Watch starts the go routine to periodically check the conntrack table
// for any discrepancies
func Watch(syncIntervalStr, metadataURL string, mc metadata.Client, dc *client.Client, debug bool) error {
	logrus.Debugf("vethsync: syncIntervalStr: %v", syncIntervalStr)

	syncInterval := DefaultSyncInterval
	if i, err := strconv.Atoi(syncIntervalStr); err == nil {
		syncInterval = i

	}

	vw := &VethWatcher{
		syncInterval: time.Duration(syncInterval) * time.Second,
		mc:           mc,
		metadataURL:  metadataURL,
		dc:           dc,
		debug:        debug,
	}

	if err := vw.runOldVethSyncOnceAtStartup(); err != nil {
		logrus.Errorf("vethsync: error running oldvethsync at startup: %v", err)
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
	hostVethMap, err := utils.GetHostViewVethMap("vethr", vw.mc)
	if err != nil {
		logrus.Errorf("vethsync: error building hostVethMap list")
		return err
	}
	logrus.Debugf("vethsync: hostVethMap: %v", hostVethMap)

	containersVethMap, err := utils.GetContainersViewVethMapUsingID(vw.dc)
	if err != nil {
		logrus.Errorf("vethsync: error building containersVethMap")
		return err
	}
	logrus.Debugf("vethsync: containersVethMap: %v", containersVethMap)

	dangling, err := utils.GetDanglingVeths(hostVethMap, containersVethMap)
	if err != nil {
		logrus.Errorf("vethsync: error checking for dangling veths: %v", err)
		return err
	}
	logrus.Debugf("vethsync: dangling: %v", dangling)

	if len(dangling) > 0 {
		utils.CleanUpDanglingVeths(dangling)
	}

	return nil
}

func (vw *VethWatcher) runOldVethSyncOnceAtStartup() error {
	cmdStr := []string{"oldvethsync", "--metadata-url", vw.metadataURL}
	if vw.debug {
		cmdStr = append(cmdStr, "--debug")
	}
	logrus.Debugf("vethsync: about to run cmd: %v", cmdStr)
	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	if err != nil {
		logrus.Errorf("vethsync: error running cmd %v: %v", cmdStr, err)
		return err
	}
	return nil
}
