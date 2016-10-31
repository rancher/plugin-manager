package network

import (
	"github.com/docker/docker/pkg/locker"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
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

	inspect, err := n.c.ContainerInspect(id)
	if client.IsErrContainerNotFound(err) {
		running = false
		time = ""
	} else if err != nil {
		return err
	} else {
		var shouldProcess bool
		inspect, shouldProcess = modifyInspect(inspect)
		if !shouldProcess {
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
	pluginState, err := glue.LookupPluginState(inspect)
	if err != nil {
		return err
	}
	if err := glue.Pre(pluginState); err != nil {
		return err
	}
	n.s.Started(id, inspect.State.StartedAt)
	return nil
}

func (n *Manager) networkDown(id string, inspect types.ContainerJSON) error {
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

func modifyInspect(inspect types.ContainerJSON) (types.ContainerJSON, bool) {
	net, ok := inspect.Config.Labels[cniLabel]
	if !ok {
		return inspect, false
	}

	inspect.HostConfig.NetworkMode = container.NetworkMode(net)
	return inspect, ok
}
