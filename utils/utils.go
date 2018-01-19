package utils

import (
	"net"
	"strings"

	"github.com/leodotcloud/log"
	"github.com/rancher/go-rancher-metadata/metadata"
)

const (
	hostLabelKeyword = "__host_label__"
)

// UpdateCNIConfigByKeywords takes in the given CNI config, replaces the rancher
// specific keywords with the appropriate values.
func UpdateCNIConfigByKeywords(config interface{}, host metadata.Host) interface{} {
	props, isMap := config.(map[string]interface{})
	if !isMap {
		return config
	}

	for aKey, aValue := range props {
		if v, isString := aValue.(string); isString {
			if strings.HasPrefix(v, hostLabelKeyword) {
				props[aKey] = ""
				splits := strings.SplitN(v, ":", 2)
				if len(splits) > 1 {
					label := strings.TrimSpace(splits[1])
					labelValue := host.Labels[label]
					if labelValue != "" {
						props[aKey] = labelValue
					}
				}
			}
		} else {
			props[aKey] = UpdateCNIConfigByKeywords(aValue, host)
		}
	}

	return props
}

// GetBridgeInfo is used to figure out the bridge information from the
// CNI config of the network specified
func GetBridgeInfo(network metadata.Network, host metadata.Host) (bridge string, bridgeSubnet string) {
	conf, _ := network.Metadata["cniConfig"].(map[string]interface{})
	for _, file := range conf {
		file = UpdateCNIConfigByKeywords(file, host)
		props, _ := file.(map[string]interface{})
		cniType, _ := props["type"].(string)
		checkBridge, _ := props["bridge"].(string)
		bridgeSubnet, _ = props["bridgeSubnet"].(string)

		if cniType == "rancher-bridge" && checkBridge != "" {
			bridge = checkBridge
			break
		}
	}
	return bridge, bridgeSubnet
}

// GetLocalNetworksAndRouters fetches networks and network containers
// related to that networks running in the current environment
func GetLocalNetworksAndRouters(networks []metadata.Network, host metadata.Host, services []metadata.Service) ([]metadata.Network, map[string]metadata.Container) {
	localRouters := map[string]metadata.Container{}
	var cniDriverServices, unfilteredCniDriverServices []metadata.Service
	var networkService metadata.Service
	// Trick to select the primary service of the network plugin
	// stack
	// TODO: Need to check if it's needed for Calico?
	for _, service := range services {
		if service.Kind == "networkDriverService" {
			unfilteredCniDriverServices = append(unfilteredCniDriverServices, service)
		}
	}

	if len(unfilteredCniDriverServices) != 1 {
		log.Debugf("found multiple cni driver services, filtering. unfilteredCniDriverServices=%v", unfilteredCniDriverServices)
		for _, service := range unfilteredCniDriverServices {
			if service.Name != "cni-driver" {
				continue
			}
			cniDriverServices = append(cniDriverServices, service)
		}
	} else {
		cniDriverServices = unfilteredCniDriverServices
	}
	log.Debugf("cniDriverServices=%v", cniDriverServices)

	if len(cniDriverServices) != 1 {
		log.Errorf("utils: error: expected one CNI driver service, but found: %v", len(cniDriverServices))
	}

	if len(cniDriverServices) > 0 {
		// Find the other service in the same stack as cniDriver
		for _, service := range services {
			if service.StackUUID == cniDriverServices[0].StackUUID &&
				service.UUID != cniDriverServices[0].UUID &&
				service.Name == service.PrimaryServiceName {
				networkService = service
				break
			}
		}
	}

	for _, aContainer := range networkService.Containers {
		if aContainer.HostUUID == host.UUID {
			localRouters[aContainer.NetworkUUID] = aContainer
		}
	}

	localNetworks := []metadata.Network{}
	for _, aNetwork := range networks {
		if aNetwork.EnvironmentUUID != host.EnvironmentUUID {
			continue
		}
		_, ok := aNetwork.Metadata["cniConfig"].(map[string]interface{})
		if !ok {
			continue
		}

		// Hack to handle multiple networks in the environment during upgrade
		// Context: pre cni-driver service release to cni-driver release
		_, ok = localRouters[aNetwork.UUID]
		if !ok {
			continue
		}

		localNetworks = append(localNetworks, aNetwork)
	}

	log.Debugf("localNetworks=%v, localRouters=%v", localNetworks, localRouters)
	return localNetworks, localRouters
}

// GetLocalNetworksAndRoutersFromMetadata is used to fetch networks local to the current environment
func GetLocalNetworksAndRoutersFromMetadata(mc metadata.Client) ([]metadata.Network, map[string]metadata.Container, error) {
	networks, err := mc.GetNetworks()
	if err != nil {
		return nil, nil, err
	}

	host, err := mc.GetSelfHost()
	if err != nil {
		return nil, nil, err
	}

	services, err := mc.GetServices()
	if err != nil {
		return nil, nil, err
	}

	networks, routers := GetLocalNetworksAndRouters(networks, host, services)

	return networks, routers, nil
}

// IsContainerConsideredRunning function is used to test if the container is in any of
// the states that are considered running.
func IsContainerConsideredRunning(aContainer metadata.Container) bool {
	return (aContainer.State == "running" || aContainer.State == "starting" || aContainer.State == "stopping")
}

// IsIPInSubnet checks if the given IP address belongs to the given subnet.
// ip without subnet mask, subnet in CIDR format
func IsIPInSubnet(ipStr, subnetStr string) (bool, error) {
	_, subnet, err := net.ParseCIDR(subnetStr)
	if err != nil {
		return false, err
	}

	ip := net.ParseIP(ipStr)

	return subnet.Contains(ip), nil
}

// IsIPInSubnets checks if the given ip address is part of any of the given subnets
func IsIPInSubnets(ip string, subnets []string) (bool, error) {
	for _, subnet := range subnets {
		in, err := IsIPInSubnet(ip, subnet)
		if err != nil {
			continue
		}
		if in {
			return true, nil
		}
	}
	return false, nil
}
