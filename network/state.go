package network

import (
	"context"
	"sync"

	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
)

type state struct {
	sync.RWMutex
	startTimes map[string]string
	c          *client.Client
}

func newState(c *client.Client) (*state, error) {
	s := &state{
		startTimes: map[string]string{},
		c:          c,
	}
	cs, err := c.ContainerList(context.Background(), types.ContainerListOptions{
		All: true,
	})
	if err != nil {
		return nil, err
	}

	for _, container := range cs {
		inspect, err := c.ContainerInspect(context.Background(), container.ID)
		if client.IsErrContainerNotFound(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		if inspect.State.Running {
			s.startTimes[container.ID] = inspect.State.StartedAt
		}
	}

	return s, nil
}

func (s *state) StartTime(id string) string {
	s.RLock()
	defer s.RUnlock()
	return s.startTimes[id]
}

func (s *state) Started(id, time string) {
	s.Lock()
	defer s.Unlock()
	s.startTimes[id] = time
}

func (s *state) Stopped(id string) {
	s.Lock()
	defer s.Unlock()
	delete(s.startTimes, id)
}
