package runner

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
)

// DockerInventory lists the executions this host's Docker holds, by the
// managed labels the runner puts on every step container and execution
// network. A remote agent serves it to the control plane, which cannot see the
// agent's Docker.
func DockerInventory(ctx context.Context) ([]ExecutionInventory, error) {
	cli, ok := sharedDockerClient()
	if !ok {
		return nil, errors.New("docker is not available on this host")
	}
	managed := filters.NewArgs(filters.Arg("label", labelManaged+"=true"))

	containers, err := cli.ContainerList(ctx, container.ListOptions{All: true, Filters: managed})
	if err != nil {
		return nil, err
	}
	networks, err := cli.NetworkList(ctx, network.ListOptions{Filters: managed})
	if err != nil {
		return nil, err
	}

	groups := inventoryGroups{}
	for _, c := range containers {
		runID := c.Labels[labelRunID]
		if runID == "" {
			continue
		}
		name := strings.TrimPrefix(firstOr(c.Names, c.ID), "/")
		var started *time.Time
		if c.Created > 0 {
			t := time.Unix(c.Created, 0).UTC()
			started = &t
		}
		run := groups.run(runID, c.Labels[labelSuite], c.Labels[labelProfile])
		run.Containers = append(run.Containers, InventoryItem{
			ID: c.ID, Name: name, Kind: "container", Image: c.Image, State: c.State, StartedAt: started,
		})
	}
	for _, n := range networks {
		runID := n.Labels[labelRunID]
		if runID == "" {
			continue
		}
		run := groups.run(runID, n.Labels[labelSuite], n.Labels[labelProfile])
		run.Networks = append(run.Networks, InventoryItem{ID: n.ID, Name: n.Name, Kind: "network"})
	}
	return groups.list(), nil
}

func firstOr(values []string, fallback string) string {
	if len(values) > 0 {
		return values[0]
	}
	return fallback
}
