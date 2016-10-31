package main

import (
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/plugin-manager/binexec"
	"github.com/rancher/plugin-manager/events"
	"github.com/rancher/plugin-manager/network"

	"github.com/rancher/plugin-manager/cniconf"
	"github.com/rancher/plugin-manager/hostports"
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

	mClient := metadata.NewClient(c.String("metadata-url"))

	if err := hostports.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start host ports configuration: %v", err)
	}

	if err := cniconf.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start cni config: %v", err)
	}

	if err := binexec.Watch(mClient); err != nil {
		logrus.Errorf("Failed to start bin config: %v", err)
	}

	dClient, err := client.NewEnvClient()
	if err != nil {
		return err
	}

	manager, err := network.NewManager(dClient)
	if err != nil {
		return err
	}

	if err := events.Watch(100, manager); err != nil {
		return err
	}

	<-make(chan struct{})
	return nil
}
