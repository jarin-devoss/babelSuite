package environments

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func (s *Service) ReapSandbox(ctx context.Context, sandboxID string) (*ReapResult, error) {
	sandboxID = strings.TrimSpace(sandboxID)
	if sandboxID == "" {
		return nil, ErrNotFound
	}
	if source, runID, ok := s.sourceFor(sandboxID); ok {
		result, err := s.reapRemote(ctx, source, runID, sandboxID)
		if err != nil {
			return nil, err
		}
		_ = s.publishLatestSnapshot(ctx, "sandbox-reap", false)
		return result, nil
	}

	containers, networks, volumes, dockerAvailable, _, err := s.collectResources(ctx)
	if err != nil {
		return nil, err
	}
	if !dockerAvailable {
		return nil, ErrDockerUnavailable
	}

	inventory := s.buildInventory(ctx, containers, networks, volumes, nil)
	var target *Sandbox
	for index := range inventory.Sandboxes {
		if inventory.Sandboxes[index].SandboxID == sandboxID {
			target = &inventory.Sandboxes[index]
			break
		}
	}
	if target == nil {
		return nil, ErrNotFound
	}

	result, err := s.reapResources(ctx, "sandbox", sandboxID, target.Containers, target.Networks, target.Volumes)
	if err != nil {
		return nil, err
	}

	_ = s.publishLatestSnapshot(ctx, "sandbox-reap", false)
	return result, nil
}

func (s *Service) ReapAll(ctx context.Context) (*ReapResult, error) {
	containers, networks, volumes, dockerAvailable, _, err := s.collectResources(ctx)
	if err != nil {
		return nil, err
	}
	if !dockerAvailable {
		if len(s.listSources()) == 0 {
			return nil, ErrDockerUnavailable
		}
		result := &ReapResult{Scope: "global", Target: "all-managed-sandboxes", Warnings: []string{"The control plane's Docker is unavailable; only remote backends were cleaned."}}
		s.reapAllRemote(ctx, result)
		result.Warnings = compactStrings(result.Warnings)
		_ = s.publishLatestSnapshot(ctx, "global-reap", false)
		return result, nil
	}

	allContainers := make([]Container, 0, len(containers))
	for _, item := range containers {
		allContainers = append(allContainers, Container{ID: item.ID, Name: item.Name})
	}
	allNetworks := make([]Network, 0, len(networks))
	for _, item := range networks {
		allNetworks = append(allNetworks, Network{ID: item.ID, Name: item.Name})
	}
	allVolumes := make([]Volume, 0, len(volumes))
	for _, item := range volumes {
		allVolumes = append(allVolumes, Volume{Name: item.Name})
	}

	result, err := s.reapResources(ctx, "global", "all-managed-sandboxes", allContainers, allNetworks, allVolumes)
	if err != nil {
		return nil, err
	}
	s.reapAllRemote(ctx, result)
	result.Warnings = compactStrings(result.Warnings)

	_ = s.publishLatestSnapshot(ctx, "global-reap", false)
	return result, nil
}

// ReapZombies removes the resources of every run on the control plane's Docker
// whose orchestrator is gone: the containers, the network and the volumes it
// left behind. A run whose orchestrator is alive is never touched, so this is
// safe to run while other control planes use the same Docker.
func (s *Service) ReapZombies(ctx context.Context) (*ReapResult, error) {
	containers, networks, volumes, dockerAvailable, _, err := s.collectResources(ctx)
	if err != nil {
		return nil, err
	}
	if !dockerAvailable {
		return nil, ErrDockerUnavailable
	}

	inventory := s.buildInventory(ctx, containers, networks, volumes, nil)
	var (
		leftContainers []Container
		leftNetworks   []Network
		leftVolumes    []Volume
		runs           int
	)
	for _, sandbox := range inventory.Sandboxes {
		if !sandbox.IsZombie {
			continue
		}
		runs++
		leftContainers = append(leftContainers, sandbox.Containers...)
		leftNetworks = append(leftNetworks, sandbox.Networks...)
		leftVolumes = append(leftVolumes, sandbox.Volumes...)
	}
	result, err := s.reapResources(ctx, "zombies", fmt.Sprintf("%d-zombie-runs", runs), leftContainers, leftNetworks, leftVolumes)
	if err != nil {
		return nil, err
	}
	if runs > 0 {
		_ = s.publishLatestSnapshot(ctx, "zombie-reap", false)
	}
	return result, nil
}

// detachSharedContainers disconnects, from the networks about to be removed,
// every container that is not itself being removed. A suite's APISIX sidecar
// serves every run of the suite and joins each run's network, and Docker will
// not remove a network that still has an endpoint on it.
func (s *Service) detachSharedContainers(ctx context.Context, networkIDs, removing []string) {
	gone := make(map[string]struct{}, len(removing))
	for _, id := range removing {
		gone[id] = struct{}{}
	}
	for _, networkID := range networkIDs {
		out, err := s.runDockerOutput(ctx, "network", "inspect", networkID)
		if err != nil {
			continue
		}
		var found []struct {
			Containers map[string]struct {
				Name string `json:"Name"`
			} `json:"Containers"`
		}
		if json.Unmarshal(out, &found) != nil {
			continue
		}
		for _, network := range found {
			for containerID := range network.Containers {
				if _, removed := gone[containerID]; removed {
					continue
				}
				_ = s.runDocker(ctx, "network", "disconnect", "-f", networkID, containerID)
			}
		}
	}
}

func (s *Service) reapResources(ctx context.Context, scope, target string, containers []Container, networks []Network, volumes []Volume) (*ReapResult, error) {
	result := &ReapResult{
		Scope:             scope,
		Target:            target,
		RemovedContainers: len(containers),
		RemovedNetworks:   len(networks),
		RemovedVolumes:    len(volumes),
		Warnings:          []string{},
	}

	if len(containers) > 0 {
		ids := make([]string, 0, len(containers))
		for _, item := range containers {
			if item.ID != "" {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) > 0 {
			if err := s.runDocker(ctx, append([]string{"rm", "-f"}, ids...)...); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Container cleanup reported an error: %s", humanizeDockerError(err)))
			}
		}
	}

	if len(networks) > 0 {
		ids := make([]string, 0, len(networks))
		for _, item := range networks {
			if item.ID != "" {
				ids = append(ids, item.ID)
			}
		}
		if len(ids) > 0 {
			removing := make([]string, 0, len(containers))
			for _, item := range containers {
				removing = append(removing, item.ID)
			}
			s.detachSharedContainers(ctx, ids, removing)
			if err := s.runDocker(ctx, append([]string{"network", "rm"}, ids...)...); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Network cleanup reported an error: %s", humanizeDockerError(err)))
			}
		}
	}

	if len(volumes) > 0 {
		names := make([]string, 0, len(volumes))
		for _, item := range volumes {
			if item.Name != "" {
				names = append(names, item.Name)
			}
		}
		if len(names) > 0 {
			if err := s.runDocker(ctx, append([]string{"volume", "rm", "-f"}, names...)...); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Volume cleanup reported an error: %s", humanizeDockerError(err)))
			}
		}
	}

	result.Warnings = compactStrings(result.Warnings)
	return result, nil
}

func (s *Service) collectResources(ctx context.Context) ([]dockerContainer, []dockerNetwork, []dockerVolume, bool, []string, error) {
	warnings := []string{}

	containers, err := s.listContainers(ctx)
	if err != nil {
		if errors.Is(err, ErrDockerUnavailable) {
			return nil, nil, nil, false, []string{humanizeDockerError(err)}, nil
		}
		warnings = append(warnings, fmt.Sprintf("Containers could not be inspected: %s", humanizeDockerError(err)))
	}

	networks, err := s.listNetworks(ctx)
	if err != nil {
		if errors.Is(err, ErrDockerUnavailable) {
			return nil, nil, nil, false, []string{humanizeDockerError(err)}, nil
		}
		warnings = append(warnings, fmt.Sprintf("Networks could not be inspected: %s", humanizeDockerError(err)))
	}

	volumes, err := s.listVolumes(ctx)
	if err != nil {
		if errors.Is(err, ErrDockerUnavailable) {
			return nil, nil, nil, false, []string{humanizeDockerError(err)}, nil
		}
		warnings = append(warnings, fmt.Sprintf("Volumes could not be inspected: %s", humanizeDockerError(err)))
	}

	return containers, networks, volumes, true, compactStrings(warnings), nil
}
