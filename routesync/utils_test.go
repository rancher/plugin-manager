package routesync

import (
	"os"
	"testing"

	"github.com/rancher/log"
)

func TestConditionsMetToWatch(t *testing.T) {
	var actual, expected bool

	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		log.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("DOCKER_BRIDGE", "docker0")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		log.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("METADATA_IP", "169.254.169.250")
	actual, _, _ = conditionsMetToWatch()
	expected = true

	if actual != expected {
		log.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("DOCKER_BRIDGE", "")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		log.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("METADATA_IP", "")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		log.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}
}

//func TestAddRouteToMetadataIP(t *testing.T) {
//	var err error
//	err = addRouteToMetadataIP("docker0", "169.254.169.250")
//	if err != nil {
//		log.Errorf("error: %v", err)
//		t.Fail()
//	}
//
//	err = addRouteToMetadataIP("eth0", "169.254.169.250")
//	if err == nil {
//		log.Errorf("expecting error, but got nil")
//		t.Fail()
//	}
//}
