package network

import (
	"context"
	"os"

	"github.com/Sirupsen/logrus"
	"github.com/docker/docker/pkg/locker"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/pkg/errors"
	glue "github.com/rancher/cniglue"
)

var cniLabel = "io.rancher.cni.network"

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
			return n.networkUp(id, inspect)
		} else if !running {
			return n.networkDown(id, inspect)
		}
	} else if running {
		return n.networkUp(id, inspect)
	}

	return nil
}

func (n *Manager) networkUp(id string, inspect types.ContainerJSON) error {
	logrus.WithFields(logrus.Fields{"networkMode": inspect.HostConfig.NetworkMode, "cid": inspect.ID}).Infof("CNI up")
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil {
		return errors.Wrap(err, "Finding plugin state")
	}
	if err := glue.Pre(pluginState); err != nil {
		return errors.Wrap(err, "Bringing up networking")
	}
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
	return glue.Post(pluginState)
}

func modifyInspect(inspect *types.ContainerJSON) bool {
	net, ok := inspect.Config.Labels[cniLabel]
	if !ok {
		return false
	}

	inspect.HostConfig.NetworkMode = container.NetworkMode(net)
	return ok
}
