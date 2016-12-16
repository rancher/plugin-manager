package reaper

import (
	"context"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/jpillora/backoff"
	"github.com/rancher/go-rancher-metadata/metadata"
)

var (
	uuidLabel        = "io.rancher.container.uuid"
	serviceNameLabel = "io.rancher.stack_service.name"
	metadataService  = "network-services/metadata"
	dnsService       = "network-services/metadata/dns"

	recheckEvery = 5 * time.Minute
)

func Watch(dockerClient *client.Client, c metadata.Client) error {
	w := &watcher{
		dc: dockerClient,
		c:  c,
	}
	go c.OnChange(5, w.onChangeNoError)
	go watchMetadata(dockerClient)
	return nil
}

func watchMetadata(dockerClient *client.Client) {
	b := &backoff.Backoff{
		Min:    1 * time.Second,
		Max:    5 * time.Minute,
		Factor: 1.5,
	}
	for {
		err := CheckMetadata(dockerClient, false)
		if err != nil {
			logrus.Errorf("Failed to check for bad metadata: %v", err)
		}
		time.Sleep(b.Duration())
	}
}

type watcher struct {
	dc *client.Client
	c  metadata.Client
}

func (w *watcher) onChangeNoError(version string) {
	if err := w.onChange(version); err != nil {
		logrus.Errorf("Failed to watch for orphan containers: %v", err)
	}
}

func (w *watcher) onChange(version string) error {
	host, err := w.c.GetSelfHost()
	if err != nil {
		return err
	}

	containers, err := w.c.GetContainers()
	if err != nil {
		return err
	}

	for _, container := range containers {
		if container.HostUUID != host.UUID {
			continue
		}
		uuid, ok := container.Labels[uuidLabel]
		if !ok {
			continue
		}

		if container.State == "running" && container.UUID != uuid {
			w.stopContainer(container)
		}
	}

	return nil
}

func CheckMetadata(dockerClient *client.Client, first bool) error {
	containers, err := dockerClient.ContainerList(context.Background(), types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return err
	}

	metadataIds := []string{}
	dnsIds := []string{}
	for _, container := range containers {
		if container.Labels[uuidLabel] != "" && container.Labels[serviceNameLabel] == metadataService {
			metadataIds = append(metadataIds, container.ID)
		}
		if container.Labels[uuidLabel] != "" && container.Labels[serviceNameLabel] == dnsService {
			dnsIds = append(dnsIds, container.ID)
		}
	}

	toDelete := []string{}

	if len(metadataIds) > 1 {
		toDelete = append(toDelete, metadataIds...)
		toDelete = append(toDelete, dnsIds...)
	} else if len(dnsIds) > 1 {
		toDelete = append(toDelete, dnsIds...)
	} else if first && len(dnsIds) == 1 {
		dnsContainer, err := dockerClient.ContainerInspect(context.Background(), dnsIds[0])
		if err != nil {
			return err
		}
		id := dnsContainer.HostConfig.NetworkMode.ConnectedContainer()
		_, err = dockerClient.ContainerInspect(context.Background(), id)
		if client.IsErrContainerNotFound(err) {
			logrus.Errorf("Failed to find network container [%s] for DNS %s", id, dnsIds[0])
			toDelete = append(toDelete, dnsIds...)
		}
	}

	for _, id := range toDelete {
		logrus.Infof("Deleting duplicate metadata/dns service: %s", id)
		err := dockerClient.ContainerRemove(context.Background(), id, types.ContainerRemoveOptions{
			Force: true,
		})
		if err != nil {
			logrus.Errorf("Failed to remove duplicate metadata/dns service: %s", id)
		}
	}

	return nil
}

func (w *watcher) stopContainer(container metadata.Container) {
	logrus.Infof("Stopping unmanaged container %s %s", container.Name, container.ExternalId)
	timeout := time.Duration(0)
	err := w.dc.ContainerStop(context.Background(), container.ExternalId, &timeout)
	if err != nil {
		logrus.Errorf("Stop failed: %v", err)
	}
}
