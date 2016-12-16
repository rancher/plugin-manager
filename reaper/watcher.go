package reaper

import (
	"context"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
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
	return nil
}

type watcher struct {
	dc                *client.Client
	c                 metadata.Client
	lastMetadataCheck time.Time
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

	if time.Now().Sub(w.lastMetadataCheck) > recheckEvery {
		if err := CheckMetadata(w.dc, false); err != nil {
			return err
		}
		w.lastMetadataCheck = time.Now()
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
