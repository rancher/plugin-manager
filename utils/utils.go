package utils

import (
	"strings"

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

// IsContainerConsideredRunning function is used to test if the container is in any of
// the states that are considered running.
func IsContainerConsideredRunning(aContainer metadata.Container) bool {
	return (aContainer.State == "running" || aContainer.State == "starting" || aContainer.State == "stopping")
}
