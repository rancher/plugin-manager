package network

import (
	"context"

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
	logrus.Debugf("Evaluating networking for: %s", id)

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
	logrus.Infof("CNI up: %s, %s", id, inspect.HostConfig.NetworkMode)
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
	if inspect.HostConfig == nil {
		return nil
	}
	logrus.Infof("CNI down: %s, %s", id, inspect.HostConfig.NetworkMode)
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil {
		return err
	}
	if err := glue.Post(pluginState); err != nil {
		return err
	}
	n.s.Stopped(id)
	return nil
}

func modifyInspect(inspect *types.ContainerJSON) bool {
	net, ok := inspect.Config.Labels[cniLabel]
	if !ok {
		logrus.Infof("Container %s does not require CNI", inspect.ID)
		return false
	}

	logrus.Infof("Container %s is using %s CNI network", inspect.ID, net)
	inspect.HostConfig.NetworkMode = container.NetworkMode(net)
	return ok
}
