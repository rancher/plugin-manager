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
	"github.com/rancher/plugin-manager/events"
	"github.com/rancher/plugin-manager/hostnat"
	"github.com/rancher/plugin-manager/hostports"
	"github.com/rancher/plugin-manager/network"
	"github.com/rancher/plugin-manager/reaper"
	"github.com/rancher/plugin-manager/routesync"
	"github.com/urfave/cli"
)

// VERSION of the binary, that can be changed during build
var VERSION = "v0.0.0-dev"

func main() {
	app := cli.NewApp()
	app.Name = "plugin-manager"
	app.Version = VERSION
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:  "metadata-url",
			Value: "http://rancher-metadata/2016-07-29",
		},
		cli.StringFlag{
			Name:  "routesync-interval",
			Usage: fmt.Sprintf("Customize the interval of routesync in seconds (default: %v)", routesync.DefaultSyncInterval),
			Value: "",
		},
		cli.StringFlag{
			Name:  "arpsync-interval",
			Usage: fmt.Sprintf("Customize the interval of arpsync in seconds (default: %v)", arpsync.DefaultSyncInterval),
			Value: "",
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

	if err := routesync.Watch(c.String("routesync-interval")); err != nil {
		logrus.Errorf("Failed to start routesync: %v", err)
		return err
	}

	dClient, err := client.NewEnvClient()
	if err != nil {
		return err
	}

	reaper.CheckMetadata(dClient, true)

	logrus.Infof("Waiting for metadata")
	mClient, err := metadata.NewClientAndWait(c.String("metadata-url"))
	if err != nil {
		return errors.Wrap(err, "Creating metadata client")
	}

	manager, err := network.NewManager(dClient)
	if err != nil {
		return err
	}

	if err := reaper.Watch(dClient, mClient); err != nil {
		logrus.Errorf("Failed to start unmanaged container reaper: %v", err)
	}

	if err := hostports.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start host ports configuration: %v", err)
	}

	if err := hostnat.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start host nat configuration: %v", err)
	}

	if err := cniconf.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start cni config: %v", err)
	}

	if err := arpsync.Watch(c.String("arpsync-interval"), mClient); err != nil {
		logrus.Errorf("Failed to start arpsync: %v", err)
	}

	binWatcher := binexec.Watch(mClient, dClient)

	if err := events.Watch(100, manager, binWatcher); err != nil {
		return err
	}

	<-make(chan struct{})
	return nil
}
