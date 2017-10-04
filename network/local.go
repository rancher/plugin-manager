package network

import (
	"context"
	"fmt"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// ForEachContainerNS is used to run the given function inside the namespace
// of all containers that are running
func ForEachContainerNS(dc *client.Client, mc metadata.Client, networkUUID string, f func(metadata.Container, ns.NetNS) error) error {
	host, err := mc.GetSelfHost()
	if err != nil {
		return errors.Wrap(err, "error fetching self host from metadata")
	}

	containers, err := mc.GetContainers()
	if err != nil {
		return errors.Wrap(err, "error fetching containers from metadata")
	}

	var lastError error
	for _, aContainer := range containers {
		if !(aContainer.HostUUID == host.UUID &&
			aContainer.State == "running" &&
			aContainer.ExternalId != "" &&
			aContainer.PrimaryIp != "" &&
			aContainer.PrimaryMacAddress != "" &&
			aContainer.NetworkUUID == networkUUID) {
			continue
		}

		err := EnterNS(dc, aContainer.ExternalId, func(n ns.NetNS) error {
			return f(aContainer, n)
		})
		if err != nil {
			lastError = err
		}
	}

	return lastError
}

// EnterNS is used to enter the given network namespace and execute
// the given function in that namespace.
func EnterNS(dc *client.Client, dockerID string, f func(ns.NetNS) error) error {
	inspect, err := dc.ContainerInspect(context.Background(), dockerID)
	if err != nil {
		return errors.Wrapf(err, "inspecting container: %v", dockerID)
	}

	containerNSStr := fmt.Sprintf("/proc/%v/ns/net", inspect.State.Pid)
	netns, err := ns.GetNS(containerNSStr)
	if err != nil {
		return errors.Wrapf(err, "failed to open netns %v", containerNSStr)
	}
	defer netns.Close()

	err = netns.Do(func(n ns.NetNS) error {
		return f(n)
	})
	if err != nil {
		return errors.Wrapf(err, "in name ns for container %s", dockerID)
	}

	return nil
}
