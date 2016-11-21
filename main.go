package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/binexec"
	"github.com/rancher/plugin-manager/cniconf"
	"github.com/rancher/plugin-manager/events"
	"github.com/rancher/plugin-manager/hostnat"
	"github.com/rancher/plugin-manager/hostports"
	"github.com/rancher/plugin-manager/migrate"
	"github.com/rancher/plugin-manager/network"
	"github.com/urfave/cli"
)

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

	logrus.Infof("Waiting for metadata")
	mClient, err := metadata.NewClientAndWait(c.String("metadata-url"))
	if err != nil {
		return errors.Wrap(err, "Creating metadata client")
	}

	dClient, err := client.NewEnvClient()
	if err != nil {
		return err
	}

	go func() {
		if err := migrate.Migrate(dClient); err != nil {
			logrus.Errorf("Failed to migrate old containers")
		}
	}()

	manager, err := network.NewManager(dClient)
	if err != nil {
		return err
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

	binWatcher := binexec.Watch(mClient, dClient)

	if err := events.Watch(100, manager, binWatcher); err != nil {
		return err
	}

	<-make(chan struct{})
	return nil
}
