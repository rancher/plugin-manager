package utils

import (
	"encoding/json"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/rancher/go-rancher-metadata/metadata"
)

// Some of the tests can run only when in development,
// remember to disable this before commiting the code.
const inDevelopment = false

func TestGetBridgeInfoFromCNIConfig(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)
	cniConf := `{"10-rancher.conf": {
  "bridge": "docker0",
  "bridgeSubnet": "10.42.0.0/16",
  "hairpinMode": true,
  "hostNat": true,
  "ipam": {
    "isDebugLevel": "false",
    "logToFile": "/var/log/rancher-cni.log",
    "routes": [
      {
        "dst": "169.254.169.250/32"
      }
    ],
    "type": "rancher-cni-ipam"
  },
  "isDebugLevel": "false",
  "isDefaultGateway": true,
  "linkMTUOverhead": 98,
  "logToFile": "/var/log/rancher-cni.log",
  "mtu": 1500,
  "name": "rancher-cni-network",
  "type": "rancher-bridge"
}}
`

	var c map[string]interface{}

	json.Unmarshal([]byte(cniConf), &c)
	logrus.Debugf("c: %#v", c)

	getBridgeInfoFromCNIConfig(c)
}

func TestGetContainersViewVethMapByEnteringNS(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)

	dClient, err := client.NewEnvClient()
	if err != nil {
		logrus.Errorf("err=%v", err)
		t.Fail()
	}
	containerVethIndices, err := GetContainersViewVethMapByEnteringNS(dClient)
	if err != nil {
		t.Fatalf("not expecting error: %v", err)
	}
	logrus.Debugf("containerVethIndices: %#v", containerVethIndices)
}

func TestGetHostViewVethMap(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)

	metadataURL := "http://169.254.169.250/2016-07-29"
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		t.Fatalf("error creating metadata client")
	}

	hostVethMap, err := GetHostViewVethMap("vethr", mc)
	if err != nil {
		t.Fatalf("vethsync: error building hostVethMap list")
	}
	logrus.Debugf("vethsync: hostVethMap: %v", hostVethMap)
}

func TestGetContainersViewVethMapUsingID(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)

	dClient, err := client.NewEnvClient()
	if err != nil {
		logrus.Errorf("err=%v", err)
		t.Fail()
	}
	containerVethIndices, err := GetContainersViewVethMapUsingID(dClient)
	if err != nil {
		t.Fatalf("not expecting error: %v", err)
	}
	logrus.Debugf("containerVethIndices: %#v", containerVethIndices)
}

func TestGetDanglingVeths(t *testing.T) {
	if !inDevelopment {
		t.Skip("not in development mode")
	}
	logrus.SetLevel(logrus.DebugLevel)

	dClient, err := client.NewEnvClient()
	if err != nil {
		logrus.Errorf("err=%v", err)
		t.Fail()
	}
	metadataURL := "http://169.254.169.250/2016-07-29"
	mc, err := metadata.NewClientAndWait(metadataURL)
	if err != nil {
		t.Fatalf("error creating metadata client")
	}
	hostVethMap, err := GetHostViewVethMap("vethr", mc)
	if err != nil {
		t.Fatalf("vethsync: error building hostVethMap list")
	}
	logrus.Debugf("vethsync: hostVethMap: %v", hostVethMap)

	containersVethMap, err := GetContainersViewVethMapUsingID(dClient)
	if err != nil {
		t.Fatalf("vethsync: error building containersVethMap")
	}
	logrus.Debugf("vethsync: containersVethMap: %v", containersVethMap)

	dangling, err := GetDanglingVeths(false, hostVethMap, containersVethMap)
	if err != nil {
		t.Fatalf("vethsync: error checking for dangling veths: %v", err)
	}
	logrus.Debugf("vethsync: dangling: %v", dangling)
}
