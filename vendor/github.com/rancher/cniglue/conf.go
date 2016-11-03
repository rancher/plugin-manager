package glue

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/container"
	"github.com/opencontainers/specs/specs-go"
)

var (
	specPaths = []string{
		"/run/docker/libcontainerd/%s/config.json",
	}
)

type DockerPluginState struct {
	ContainerID string
	State       specs.State
	Spec        specs.Spec
	HostConfig  container.HostConfig
	Config      container.Config
}

func ReadState() (*DockerPluginState, error) {
	pluginState := DockerPluginState{}
	config := struct {
		ID     string
		Config container.Config
	}{}

	if err := json.NewDecoder(os.Stdin).Decode(&pluginState.State); err != nil {
		return nil, err
	}

	if err := readJSONFile(os.Getenv("DOCKER_HOST_CONFIG"), &pluginState.HostConfig); err != nil {
		return nil, err
	}

	if err := readJSONFile(os.Getenv("DOCKER_CONFIG"), &config); err != nil {
		return nil, err
	}

	pluginState.Config = config.Config
	pluginState.ContainerID = config.ID

	return &pluginState, readJSONFile(path.Join(pluginState.State.BundlePath, "config.json"), &pluginState.Spec)
}

func FindSpec(id string) (string, *specs.Spec, error) {
	var spec specs.Spec

	for _, p := range specPaths {
		configJSON := fmt.Sprintf(p, id)
		f, err := os.Open(configJSON)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return "", nil, err
		}
		if err := json.NewDecoder(f).Decode(&spec); err != nil {
			return "", nil, err
		}
		return path.Dir(configJSON), &spec, nil
	}

	return "", nil, os.ErrNotExist
}

func LookupPluginState(container types.ContainerJSON) (*DockerPluginState, error) {
	result := &DockerPluginState{}

	bundlePath, spec, err := FindSpec(container.ID)
	if err != nil {
		return nil, err
	}
	result.ContainerID = container.ID
	result.Spec = *spec
	result.HostConfig = *container.HostConfig
	result.Config = *container.Config
	result.State = specs.State{
		BundlePath: bundlePath,
		ID:         container.ID,
		Pid:        container.State.Pid,
	}
	return result, nil
}

func FindSpecState(id string) (*specs.Spec, error) {
	var spec specs.Spec

	for _, p := range specPaths {
		f, err := os.Open(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if err := json.NewDecoder(f).Decode(&spec); err != nil {
			return nil, err
		}
		return &spec, nil
	}

	return nil, os.ErrNotExist
}
