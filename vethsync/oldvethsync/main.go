package main

import (
	"os"

	"github.com/docker/engine-api/client"
	"github.com/rancher/go-rancher-metadata/metadata"
	"github.com/rancher/log"
	"github.com/rancher/plugin-manager/vethsync/utils"
	"github.com/urfave/cli"
)

// VERSION of the binary, that can be changed during build
var VERSION = "v0.0.0-dev"

func main() {
	app := cli.NewApp()
	app.Name = "oldvethsync"
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
		log.SetLevelString("debug")
	}

	dClient, err := client.NewEnvClient()
	if err != nil {
		return err
	}

	log.Infof("Waiting for metadata")
	mClient, err := metadata.NewClientAndWait(c.String("metadata-url"))
	if err != nil {
		log.Errorf("oldvethsync: error creating metadata client: %v", err)
		return err
	}

	if err := doSync(mClient, dClient); err != nil {
		log.Errorf("oldvethsync: failed with error: %v", err)
		return err
	}

	return nil
}

func doSync(mc metadata.Client, dc *client.Client) error {
	log.Debugf("oldvethsync: doSync")

	hostVethMap, err := utils.GetHostViewVethMap("veth", mc)
	if err != nil {
		log.Errorf("oldvethsync: error building hostVethMap list")
		return err
	}
	log.Debugf("oldvethsync: hostVethMap: %v", hostVethMap)

	containersVethMap, err := utils.GetContainersViewVethMapByEnteringNS(dc)
	if err != nil {
		log.Errorf("oldvethsync: error building containersVethMap")
		return err
	}
	log.Debugf("oldvethsync: containersVethMap: %v", containersVethMap)

	dangling, err := utils.GetDanglingVeths(true, hostVethMap, containersVethMap)
	if err != nil {
		log.Errorf("oldvethsync: error checking for dangling veths: %v", err)
		return err
	}
	log.Debugf("oldvethsync: dangling: %v", dangling)

	if len(dangling) > 0 {
		utils.CleanUpDanglingVeths(dangling)
	}

	return nil
}
