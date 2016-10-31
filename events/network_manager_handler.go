package events

import (
	docker "github.com/fsouza/go-dockerclient"
	"github.com/rancher/plugin-manager/network"
)

type NetworkManagerHandler struct {
	nm *network.Manager
}

func (h *NetworkManagerHandler) Handle(event *docker.APIEvents) error {
	return h.nm.Evaluate(event.ID)
}
