package events

import (
	log "github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
	"github.com/rancher/plugin-manager/binexec"
	"github.com/rancher/plugin-manager/network"
)

const (
	simulatedEvent = "-simulated-"
)

func Watch(poolSize int, nm *network.Manager, bw *binexec.Watcher, disableDNSSetup bool) error {
	dep := &DockerEventsProcessor{
		poolSize:        poolSize,
		nm:              nm,
		bw:              bw,
		disableDNSSetup: disableDNSSetup,
	}
	return dep.Process()
}

type DockerEventsProcessor struct {
	poolSize        int
	nm              *network.Manager
	bw              *binexec.Watcher
	disableDNSSetup bool
}

func (de *DockerEventsProcessor) Process() error {
	dockerClient, err := NewDockerClient()
	if err != nil {
		return err
	}

	nmHandler := &NetworkManagerHandler{de.nm}
	var startHandler *StartHandler
	if !de.disableDNSSetup {
		log.Infof("enabling dns setup")
		startHandler = &StartHandler{dockerClient}
	} else {
		log.Infof("disabling dns setup")
	}
	handlers := map[string][]Handler{
		"start": {
			de.bw,
			startHandler,
			nmHandler,
		},
		"die": {
			nmHandler,
		},
	}

	router, err := NewEventRouter(de.poolSize, de.poolSize, dockerClient, handlers)
	if err != nil {
		return err
	}
	router.Start()

	containers, err := dockerClient.ListContainers(docker.ListContainersOptions{
		All: true,
	})
	if err != nil {
		return err
	}

	for _, c := range containers {
		event := &docker.APIEvents{
			ID:     c.ID,
			Status: "start",
			From:   simulatedEvent,
		}
		router.listener <- event
	}

	return nil
}
