package conntracksync

import (
	"strconv"
	"time"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/conntracksync/conntrack"
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
	log.Debugf("conntracksync: syncIntervalStr: %v", syncIntervalStr)

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
	log.Debugf("conntracksync: metadata version: %v, lastApplied: %v", version, ctw.lastApplied)
	timeSinceLastApplied := time.Now().Sub(ctw.lastApplied)
	if timeSinceLastApplied < ctw.syncInterval {
		timeToSleep := ctw.syncInterval - timeSinceLastApplied
		log.Debugf("conntracksync: sleeping for %v", timeToSleep)
		time.Sleep(timeToSleep)
	}
	if err := ctw.doSync(); err != nil {
		log.Errorf("conntracksync: while syncing, got error: %v", err)
	}
	ctw.lastApplied = time.Now()
}

func (ctw *ConntrackTableWatcher) doSync() error {
	return conntrack.SyncNATEntries(ctw.mc)
}
