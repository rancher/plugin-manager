package network

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	cniTypes "github.com/containernetworking/cni/pkg/types"
	"github.com/docker/docker/pkg/locker"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/pkg/errors"
	glue "github.com/rancher/cniglue"
)

const (
	maxRetries            = 15
	IPLabel               = "io.rancher.container.ip"
	LegacyManagedNetLabel = "io.rancher.container.network"
	CNILabel              = "io.rancher.cni.network"
	rootStateDir          = "/var/lib/rancher/state/cni"
)

type Manager struct {
	c     *client.Client
	s     *state
	locks *locker.Locker
}

func NewManager(c *client.Client) (*Manager, error) {
	s, err := newState(rootStateDir, c)
	if err != nil {
		return nil, err
	}
	return &Manager{
		c:     c,
		s:     s,
		locks: locker.New(),
	}, nil
}

// Evaluate checks the state and enables networking if needed
func (n *Manager) Evaluate(id string) error {
	return n.evaluate(id, 0)
}

func (n *Manager) evaluate(id string, retryCount int) error {
	n.locks.Lock(id)
	defer n.locks.Unlock(id)

	wasTime := n.s.StartTime(id)
	wasRunning := wasTime != ""
	running := false
	time := ""

	inspect, err := n.c.ContainerInspect(context.Background(), id)
	if client.IsErrContainerNotFound(err) {
		running = false
		time = ""
	} else if err != nil {
		return err
	} else {
		if !configureNetwork(&inspect) {
			return nil
		}
		running = inspect.State.Running
		time = inspect.State.StartedAt
	}

	logrus.WithFields(logrus.Fields{
		"wasTime":    wasTime,
		"wasRunning": wasRunning,
		"running":    running,
		"time":       time,
		"cid":        id,
	}).Debugf("Evaluating networking start")

	if wasRunning {
		if running && wasTime != time {
			return n.networkUp(id, inspect, retryCount)
		} else if !running {
			return n.networkDown(id, inspect)
		}
	} else if running {
		return n.networkUp(id, inspect, retryCount)
	}

	return nil
}

func (n *Manager) retry(id string, retryCount int) {
	time.Sleep(2 * time.Second)
	logrus.WithFields(logrus.Fields{"cid": id, "count": retryCount}).Infof("Evaluating state from retry")
	if err := n.evaluate(id, retryCount); err != nil {
		logrus.Errorf("Failed to evaluate networking: %v", err)
	}
}

func (n *Manager) networkUp(id string, inspect types.ContainerJSON, retryCount int) (err error) {
	logrus.WithFields(logrus.Fields{"networkMode": inspect.HostConfig.NetworkMode, "cid": inspect.ID}).Infof("CNI up")
	startedAt := inspect.State.StartedAt

	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil {
		return n.s.recordNetworkUpError(id, startedAt, errors.Wrap(err, "Couldn't find plugin state"))
	}
	result, err := glue.CNIAdd(pluginState)
	if err != nil || result == nil {
		if retryCount < maxRetries {
			go n.retry(id, retryCount+1)
			return err
		}
		return n.s.recordNetworkUpError(id, startedAt, errors.Wrap(err, "Couldn't bring up network"))
	}
	logrus.WithFields(logrus.Fields{
		"networkMode": inspect.HostConfig.NetworkMode,
		"cid":         inspect.ID,
		"result":      result,
	}).Infof("CNI up done")
	if err := n.setupHosts(inspect, result); err != nil {
		return n.s.recordNetworkUpError(id, startedAt, errors.Wrap(err, "Couldn't setup hosts"))
	}
	n.s.Started(id, inspect.State.StartedAt, result)
	return nil
}

func (n *Manager) setupHosts(inspect types.ContainerJSON, result *cniTypes.Result) error {
	if inspect.Config == nil || inspect.Config.Hostname == "" || inspect.HostsPath == "" ||
		result == nil || result.IP4.IP.String() == "" {
		return nil
	}

	hosts, err := ioutil.ReadFile(inspect.HostsPath)
	ip := strings.SplitN(result.IP4.IP.String(), "/", 2)[0]
	if os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}

	hostsString := string(hosts)
	line := fmt.Sprintf("\n%s\t%s\n", ip, inspect.Config.Hostname)
	if strings.Contains(hostsString, line) {
		return nil
	}

	updatedHosts := hostsString + line
	return ioutil.WriteFile(inspect.HostsPath, []byte(updatedHosts), 0644)
}

func (n *Manager) networkDown(id string, inspect types.ContainerJSON) error {
	defer n.s.Stopped(id)
	if inspect.ContainerJSONBase == nil || inspect.HostConfig == nil {
		return nil
	}
	logrus.WithFields(logrus.Fields{"networkMode": inspect.HostConfig.NetworkMode, "cid": inspect.ID}).Infof("CNI down")
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "Finding plugin state on down")
	}
	return glue.CNIDel(pluginState)
}

func configureNetwork(inspect *types.ContainerJSON) bool {
	logrus.Debugf("inpect.HostConfig: %+v inpect.Config: %+v inpect.NetworkSettings: %+v",
		*inspect.HostConfig, *inspect.Config, *inspect.NetworkSettings)
	net, ok := inspect.Config.Labels[CNILabel]
	if !ok &&
		!(string(inspect.HostConfig.NetworkMode) == "host" || strings.HasPrefix(string(inspect.HostConfig.NetworkMode), "container")) &&
		(inspect.Config.Labels[LegacyManagedNetLabel] == "true" || inspect.Config.Labels[IPLabel] != "") {
		net = "managed"
	}

	if net == "" {
		return false
	}

	inspect.HostConfig.NetworkMode = container.NetworkMode(net)
	return true
}
