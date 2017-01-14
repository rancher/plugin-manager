package network

import (
	"context"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
)

type state struct {
	sync.RWMutex
	rootStateDir string
	startTimes   map[string]string
	c            *client.Client
}

func newState(rootStateDir string, c *client.Client) (*state, error) {
	s := &state{
		startTimes:   map[string]string{},
		c:            c,
		rootStateDir: rootStateDir,
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
		logrus.WithFields(logrus.Fields{
			"cid":       container.ID,
			"running":   inspect.State.Running,
			"startedAt": inspect.State.StartedAt,
		}).Infof("Inspecting on start")
		if inspect.State.Running {
			hasIface, err := s.hasNetwork(inspect.State.Pid)
			if err != nil {
				logrus.WithField("cid", inspect.ID).Errorf("Failed to inspect interfaces")
				continue
			}
			if hasIface {
				logrus.WithFields(logrus.Fields{
					"cid":       container.ID,
					"startedAt": inspect.State.StartedAt,
				}).Info("Recording previously started")
				s.Started(container.ID, inspect.State.StartedAt, nil)
			} else {
				logrus.WithFields(logrus.Fields{
					"cid": container.ID,
				}).Info("Still needs networking")
			}
		}
	}

	return s, nil
}

func (s *state) StartTime(id string) string {
	s.RLock()
	defer s.RUnlock()
	return s.startTimes[id]
}

func (s *state) Started(id, startedAt string, networkData interface{}) {
	s.Lock()
	s.startTimes[id] = startedAt
	s.Unlock()
	if networkData != nil {
		s.writeState(id, startedAt, networkData)
	}
}

func (s *state) writeState(id, startedAt string, state interface{}) {
	dir := path.Join(s.rootStateDir, id)
	filename := path.Join(dir, startedAt)

	data, err := json.Marshal(state)
	if err != nil {
		logrus.Warnf("Problem marshaling network data for %v: %v", filename, err)
		return
	}

	if err := os.MkdirAll(dir, 0644); err != nil {
		logrus.Warnf("Problem creating network state dir for %v: %v", filename, err)
		return
	}

	f, err := ioutil.TempFile(dir, startedAt)
	if err != nil {
		logrus.Warnf("Problem creating network data temp file for %v: %v", filename, err)
		return
	}

	_, err = f.Write(data)
	if err != nil {
		logrus.Warnf("Problem writing network data to temp file for %v: %v", filename, err)
		return
	}
	defer f.Close()

	if err := os.Rename(f.Name(), filename); err != nil {
		logrus.Warnf("Problem renaming network data file for %v: %v", filename, err)
	}
}

func (s *state) Stopped(id string) {
	s.Lock()
	defer s.Unlock()
	delete(s.startTimes, id)

	dirName := path.Join(s.rootStateDir, id)
	if err := os.RemoveAll(dirName); err != nil {
		logrus.Warnf("Problem cleaning up network state dir %v: %v", dirName, err)
	}
}

// The returned error is the passed in error. It does not represent a problem
func (s *state) recordNetworkUpError(id, startedAt string, err error) error {
	errData := map[string]string{
		"error": err.Error(),
	}
	s.writeState(id, startedAt, errData)
	return err
}
