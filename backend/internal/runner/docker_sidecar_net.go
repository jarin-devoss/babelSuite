package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/network"

)

// sidecarConfigHashLabel carries a digest of everything the sidecar was started
// with, so a sidecar whose configuration has since changed is recreated.
const sidecarConfigHashLabel = "babelsuite.config-hash"

// sidecarConfigHash digests the image and every file the sidecar mounts: its
// config.yaml, its routes and each Lua plugin. A running sidecar reads all of
// them only at startup, so without this a fix to any of them would never reach
// a sidecar that is already up.
func sidecarConfigHash(image, configYAML, apisixYAML string, luaFiles map[string]string) string {
	h := sha256.New()
	write := func(label, content string) {
		fmt.Fprintf(h, "%s\x00%d\x00%s\x00", label, len(content), content)
	}
	write("image", image)
	// The sidecar publishes a host port chosen before it starts. One started by
	// an older build took whatever port Docker picked, which moves when a run
	// network takes the gateway, so it is recreated once.
	write("ports", "fixed")
	write("config.yaml", configYAML)
	write("apisix.yaml", apisixYAML)

	names := make([]string, 0, len(luaFiles))
	for name := range luaFiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		write("lua/"+name, luaFiles[name])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// AttachSidecarToExecution joins the suite's long-lived APISIX sidecar to an
// execution's network, so the sidecar can reach that run's service containers
// directly. The sidecar serves every execution of a suite and profile, so it
// may sit on several run networks at once; RemoveExecutionNetwork detaches it
// when the run ends. Attaching twice is not an error.
func AttachSidecarToExecution(ctx context.Context, suiteID, profile, executionID string, aliases ...string) error {
	cli, ok := sharedDockerClient()
	if !ok {
		return fmt.Errorf("docker is not available")
	}
	sidecar := SidecarContainerName(suiteID, profile)
	netName := executionNetworkName(executionID)

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// GwPriority -1 keeps the run network from becoming the sidecar's default
	// gateway. Docker gives that role to the highest priority and, on a tie, to
	// the network whose name sorts first, and babel-net-... sorts before the
	// default bridge. The published host port lives on the gateway endpoint, so
	// a network that took it could move the address every client of the sidecar
	// was given.
	err := cli.NetworkConnect(ctx, netName, sidecar, &network.EndpointSettings{GwPriority: -1, Aliases: aliases})
	if err == nil || errdefs.IsConflict(err) || strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return nil
	}
	return fmt.Errorf("attach sidecar %s to %s: %w", sidecar, netName, err)
}

// GatewayAlias is the name a step's container uses for the suite's sidecar on
// the run's network. The sidecar's address on the host (127.0.0.1:<port>) means
// the container itself from inside one.
const GatewayAlias = "babelsuite-gateway"

// SidecarAttacher is implemented by a backend whose steps run on a Docker
// network the sidecar can join.
type SidecarAttacher interface {
	// AttachSidecar joins the suite's sidecar to the run's network under
	// GatewayAlias and the given mock host names, so a step reaches its mock
	// surfaces through the sidecar and the sidecar can tell which run asked.
	//
	// It returns the address the sidecar has on that network, which is the one
	// a request from the run arrives on.
	AttachSidecar(ctx context.Context, suiteID, profile, executionID string, mockHosts []string) (string, error)
}

// SidecarAddressOnExecutionNetwork returns the sidecar's address on the run's
// network.
func SidecarAddressOnExecutionNetwork(ctx context.Context, suiteID, profile, executionID string) (string, error) {
	cli, ok := sharedDockerClient()
	if !ok {
		return "", fmt.Errorf("docker is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	sidecar := SidecarContainerName(suiteID, profile)
	info, err := cli.ContainerInspect(ctx, sidecar)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", sidecar, err)
	}
	if info.NetworkSettings != nil {
		if endpoint, found := info.NetworkSettings.Networks[executionNetworkName(executionID)]; found && endpoint.IPAddress != "" {
			return endpoint.IPAddress, nil
		}
	}
	return "", fmt.Errorf("%s has no address on the run's network", sidecar)
}

// sidecarAttached reports whether the suite's sidecar is on the run's network.
func sidecarAttached(ctx context.Context, step StepSpec) bool {
	cli, ok := sharedDockerClient()
	if !ok || step.SuiteID == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	info, err := cli.NetworkInspect(ctx, executionNetworkName(step.ExecutionID), network.InspectOptions{})
	if err != nil {
		return false
	}
	sidecar := SidecarContainerName(step.SuiteID, step.Profile)
	for _, endpoint := range info.Containers {
		if endpoint.Name == sidecar {
			return true
		}
	}
	return false
}

// ContainerIPOnExecutionNetwork returns the address of a step's container on
// the execution network. The sidecar dials that address rather than the
// container's name: it needs no DNS (the sidecar was started on the default
// network, so its resolver does not know the run network's names), and it is
// unambiguous when two runs of one suite are both attached to the sidecar.
func ContainerIPOnExecutionNetwork(ctx context.Context, executionID, nodeID string) (string, error) {
	cli, ok := sharedDockerClient()
	if !ok {
		return "", fmt.Errorf("docker is not available")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	name := fmt.Sprintf("babel-%s-%s", sanitizeID(executionID), sanitizeID(nodeID))
	info, err := cli.ContainerInspect(ctx, name)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", name, err)
	}
	if info.State == nil || !info.State.Running {
		return "", fmt.Errorf("container %s is not running", name)
	}
	if info.NetworkSettings != nil {
		if endpoint, found := info.NetworkSettings.Networks[executionNetworkName(executionID)]; found && endpoint.IPAddress != "" {
			return endpoint.IPAddress, nil
		}
	}
	return "", fmt.Errorf("container %s has no address on the execution network", name)
}
