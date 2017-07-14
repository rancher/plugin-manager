package network

import (
	"context"
	"fmt"

	"github.com/containernetworking/cni/pkg/ns"
	"github.com/docker/engine-api/client"
	"github.com/pkg/errors"
	"github.com/rancher/go-rancher-metadata/metadata"
)

func LocalNetworks(mc metadata.Client) ([]metadata.Network, map[string]metadata.Container, error) {
	networks, err := mc.GetNetworks()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error fetching networks from metadata")
	}

	host, err := mc.GetSelfHost()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error fetching self host from metadata")
	}

	services, err := mc.GetServices()
	if err != nil {
		return nil, nil, errors.Wrap(err, "error fetching services from metadata")
	}

	routers := map[string]metadata.Container{}
	for _, service := range services {
		// Trick to select the primary service of the network plugin
		// stack
		// TODO: Need to check if it's needed for Calico?
		if !(service.Kind == "networkDriverService" &&
			service.Name == service.PrimaryServiceName) {
			continue
		}

		for _, aContainer := range service.Containers {
			if aContainer.HostUUID == host.UUID {
				routers[aContainer.NetworkUUID] = aContainer
			}
		}
	}

	ret := []metadata.Network{}
	for _, aNetwork := range networks {
		if aNetwork.EnvironmentUUID != host.EnvironmentUUID {
			continue
		}
		_, ok := aNetwork.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}
		ret = append(ret, aNetwork)
	}

	return ret, routers, nil
}

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
