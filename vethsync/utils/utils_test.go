package utils

import (
	"encoding/json"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
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
