package migrate

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/filters"
)

func Migrate(dc *client.Client) error {
	args := filters.NewArgs()
	args.Add("label", "io.rancher.container.ip")
	args.Add("label", "io.rancher.container.uuid")
	containers, err := dc.ContainerList(context.Background(), types.ContainerListOptions{
		Filter: args,
	})
	if err != nil {
		return err
	}

	var lastErr error
	for _, container := range containers {
		if _, ok := container.Labels["io.rancher.cni.network"]; !ok {
			err := removeRoute(dc, container.ID)
			if err != nil {
				lastErr = err
			}
		}
	}

	return lastErr
}

func removeRoute(dc *client.Client, id string) error {
	inspect, err := dc.ContainerInspect(context.Background(), id)
	if err != nil {
		return err
	}

	if inspect.State == nil || inspect.State.Pid == 0 {
		return nil
	}

	cmd := exec.Command("nsenter", "-n", "-t", fmt.Sprint(inspect.State.Pid), "--",
		"ip", "route", "del", "169.254.169.250/32")
	cmd.Run()
	return nil
}
