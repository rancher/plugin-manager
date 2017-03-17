package routesync

import (
	"os"
	"testing"

	"github.com/Sirupsen/logrus"
)

func TestConditionsMetToWatch(t *testing.T) {
	var actual, expected bool

	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		logrus.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("DOCKER_BRIDGE", "docker0")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		logrus.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("METADATA_IP", "169.254.169.250")
	actual, _, _ = conditionsMetToWatch()
	expected = true

	if actual != expected {
		logrus.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("DOCKER_BRIDGE", "")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		logrus.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}

	os.Setenv("METADATA_IP", "")
	actual, _, _ = conditionsMetToWatch()
	expected = false

	if actual != expected {
		logrus.Errorf("expected: %v, got actual: %v", expected, actual)
		t.Fail()
	}
}

//func TestAddRouteToMetadataIP(t *testing.T) {
//	var err error
//	err = addRouteToMetadataIP("docker0", "169.254.169.250")
//	if err != nil {
//		logrus.Errorf("error: %v", err)
//		t.Fail()
//	}
//
//	err = addRouteToMetadataIP("eth0", "169.254.169.250")
//	if err == nil {
//		logrus.Errorf("expecting error, but got nil")
//		t.Fail()
//	}
//}
