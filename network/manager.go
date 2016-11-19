package network

import (
	"context"
	"os"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/locker"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/pkg/errors"
	glue "github.com/rancher/cniglue"
)

var (
	cniLabel   = "io.rancher.cni.network"
	maxRetries = 60
)

type Manager struct {
	c     *client.Client
	s     *state
	locks *locker.Locker
}

func NewManager(c *client.Client) (*Manager, error) {
	s, err := newState(c)
	if err != nil {
		return nil, err
	}
	return &Manager{
		c:     c,
		s:     s,
		locks: locker.New(),
	}, nil
}

// Evaluate checks the state and enableds networking if needed
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
		if !modifyInspect(&inspect) {
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
	logrus.WithField("cid", id).Infof("Evaluating state from retry")
	if err := n.evaluate(id, retryCount); err != nil {
		logrus.Errorf("Failed to evaluate networking: %v", err)
	}
}

func (n *Manager) networkUp(id string, inspect types.ContainerJSON, retryCount int) error {
	logrus.WithFields(logrus.Fields{"networkMode": inspect.HostConfig.NetworkMode, "cid": inspect.ID}).Infof("CNI up")
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil {
		return errors.Wrap(err, "Finding plugin state")
	}
	result, err := glue.CNIAdd(pluginState)
	if err != nil {
		if retryCount < maxRetries {
			go n.retry(id, retryCount+1)
		}
		return errors.Wrap(err, "Bringing up networking")
	}
	logrus.WithFields(logrus.Fields{
		"networkMode": inspect.HostConfig.NetworkMode,
		"cid":         inspect.ID,
		"result":      result,
	}).Infof("CNI up done")
	n.s.Started(id, inspect.State.StartedAt)
	return nil
}

func (n *Manager) networkDown(id string, inspect types.ContainerJSON) error {
	defer n.s.Stopped(id)
	if inspect.HostConfig == nil {
		return nil
	}
	logrus.WithFields(logrus.Fields{"networkMode": inspect.HostConfig.NetworkMode, "cid": inspect.ID}).Infof("CNI down")
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil && !os.IsNotExist(err) {
		return errors.Wrap(err, "Finding plugin state on down")
	}
	return glue.CNIDel(pluginState)
}

func modifyInspect(inspect *types.ContainerJSON) bool {
	net, ok := inspect.Config.Labels[cniLabel]
	if !ok {
		return false
	}

	inspect.HostConfig.NetworkMode = container.NetworkMode(net)
	return ok
}
