package main

import (
	"fmt"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/arpsync"
	"github.com/rancher/plugin-manager/binexec"
	"github.com/rancher/plugin-manager/cniconf"
	"github.com/rancher/plugin-manager/conntracksync"
	"github.com/rancher/plugin-manager/events"
	"github.com/rancher/plugin-manager/hostnat"
	"github.com/rancher/plugin-manager/hostports"
	"github.com/rancher/plugin-manager/iptablessync"
	"github.com/rancher/plugin-manager/macsync"
	"github.com/rancher/plugin-manager/network"
	"github.com/rancher/plugin-manager/reaper"
	"github.com/rancher/plugin-manager/routesync"
	"github.com/rancher/plugin-manager/vethsync"
	"github.com/urfave/cli"
)

const (
	metadataURLTemplate = "http://%v/2016-07-29"
)

// VERSION of the binary, that can be changed during build
var VERSION = "v0.0.0-dev"

func main() {
	app := cli.NewApp()
	app.Name = "plugin-manager"
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "metadata-address",
			Value: "169.254.169.250",
		},
		cli.StringFlag{
			Name:   "metadata-listen-port",
			EnvVar: "RANCHER_METADATA_LISTEN_PORT",
			Value:  "80",
		},
		cli.BoolFlag{
			Name:  "disable-macsync",
			Usage: "Disable macsync",
		},
		cli.BoolFlag{
			Name:  "disable-conntracksync",
			Usage: "Disable conntracksync",
		},
		cli.StringFlag{
			Name:  "conntracksync-interval",
			Usage: fmt.Sprintf("Customize the interval of conntracksync in seconds (default: %v)", conntracksync.DefaultSyncInterval),
			Value: "",
		},
		cli.BoolFlag{
			Name:  "disable-routesync",
			Usage: "Disable routesync",
		},
		cli.StringFlag{
			Name:  "routesync-interval",
			Usage: fmt.Sprintf("Customize the interval of routesync in seconds (default: %v)", routesync.DefaultSyncInterval),
			Value: "",
		},
		cli.BoolFlag{
			Name:  "disable-arpsync",
			Usage: "Disable arpsync",
		},
		cli.StringFlag{
			Name:  "arpsync-interval",
			Usage: fmt.Sprintf("Customize the interval of arpsync in seconds (default: %v)", arpsync.DefaultSyncInterval),
			Value: "",
		},
		cli.BoolFlag{
			Name:  "disable-vethsync",
			Usage: "Disable vethsync",
		},
		cli.StringFlag{
			Name:  "vethsync-interval",
			Usage: fmt.Sprintf("Customize the interval of vethsync in seconds (default: %v)", vethsync.DefaultSyncInterval),
			Value: "",
		},
		cli.IntFlag{
			Name:  "iptables-sync-interval",
			Usage: fmt.Sprintf("Customize the interval of iptables-sync in seconds (default: %v)", iptablessync.DefaultSyncInterval),
			Value: iptablessync.DefaultSyncInterval,
		},
		cli.BoolFlag{
			Name:  "disable-cni-setup",
			Usage: "Disable setting up CNI config and binaries",
		},
		cli.BoolFlag{
			Name:  "debug",
			Usage: "Turn on debug logging",
		},
	}
	app.Action = run
	app.Run(os.Args)
}

func run(c *cli.Context) error {
	if c.Bool("debug") {
		logrus.SetLevel(logrus.DebugLevel)
	}

	if !c.Bool("disable-routesync") {
		if err := routesync.Watch(c.String("routesync-interval")); err != nil {
			logrus.Errorf("Failed to start routesync: %v", err)
			return err
		}
	}

	dClient, err := client.NewEnvClient()
	if err != nil {
		return err
	}

	reaper.CheckMetadata(dClient)

	metadataURL := fmt.Sprintf(metadataURLTemplate, c.String("metadata-address"))
	logrus.Infof("Waiting for metadata")
	mClient, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		return errors.Wrap(err, "Creating metadata client")
	}

	if !c.Bool("disable-macsync") {
		macsync.SyncMACAddresses(mClient, dClient)
	}

	manager, err := network.NewManager(dClient)
	if err != nil {
		return err
	}

	if err := reaper.Watch(dClient, mClient); err != nil {
		logrus.Errorf("Failed to start unmanaged container reaper: %v", err)
	}

	if err := iptablessync.Watch(c.Int("iptables-sync-interval"), mClient); err != nil {
		logrus.Errorf("Failed to start host ports configuration: %v", err)
	}

	if err := hostports.Watch(mClient, c.String("metadata-address"), c.String("metadata-listen-port")); err != nil {
		logrus.Errorf("Failed to start host ports configuration: %v", err)
	}

	if err := hostnat.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start host nat configuration: %v", err)
	}

	if !c.Bool("disable-conntracksync") {
		if err := conntracksync.Watch(c.String("conntracksync-interval"), mClient); err != nil {
			logrus.Errorf("Failed to start conntracksync: %v", err)
		}
	}

	if !c.Bool("disable-cni-setup") {
		if err := cniconf.Watch(mClient); err != nil {
			logrus.Errorf("Failed to start cni config: %v", err)
		}
	}

	if !c.Bool("disable-arpsync") {
		if err := arpsync.Watch(c.String("arpsync-interval"), mClient, dClient); err != nil {
			logrus.Errorf("Failed to start arpsync: %v", err)
		}
	}

	if !c.Bool("disable-vethsync") {
		if err := vethsync.Watch(c.String("vethsync-interval"), metadataURL, mClient, dClient, c.Bool("debug")); err != nil {
			logrus.Errorf("Failed to start vethsync: %v", err)
		}
	}

	var binWatcher *binexec.Watcher
	if !c.Bool("disable-cni-setup") {
		binWatcher = binexec.Watch(mClient, dClient)
	}

	if err := events.Watch(100, manager, binWatcher); err != nil {
		return err
	}

	<-make(chan struct{})
	return nil
}
