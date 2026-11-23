package runner

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"net/url"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

const (
	containerWorkspaceMount = "/babelsuite/workspace"
	containerMemoryLimit    = 512 * 1024 * 1024 // 512 MB per step container
	// Docker refuses a memory limit under 6 MB. Checking it here turns a
	// profile typo into a named error instead of an opaque daemon rejection.
	minContainerMemoryLimit = 6 * 1024 * 1024
	containerPidsLimit      = int64(256)
)

// ExecutionWorkspaceDir returns the host path of the shared workspace
// directory for an execution. Every container in the execution mounts this
// directory so steps can exchange files without artifact export configuration.
func ExecutionWorkspaceDir(executionID string) string {
	return filepath.Join(os.TempDir(), "babel-workspace", sanitizeID(executionID))
}

// executionNetworkName returns the Docker network name for an execution.
func executionNetworkName(executionID string) string {
	return "babel-net-" + sanitizeID(executionID)
}

// ExecutionNetworkName is the exported form of executionNetworkName for use
// outside the runner package.
func ExecutionNetworkName(executionID string) string {
	return executionNetworkName(executionID)
}

// SetupExecutionNetwork creates the per-execution bridge network once, before
// any container goroutines start. Call this before enqueuing tasks so that
// every concurrent runInDocker call finds the network already present.
//
// The error is returned rather than dropped: Docker has a finite number of
// subnets to hand out, and when they run out the failure here is the only place
// that says so. Swallowed, it resurfaces later as every container reporting
// "network <name> not found", which describes a missing network rather than an
// exhausted address pool and sends you looking in the wrong place.
func SetupExecutionNetwork(executionID string) error {
	cli, ok := sharedDockerClient()
	if !ok {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := executionNetworkName(executionID)
	if _, err := cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: "bridge", Labels: managedLabels(executionID, nil)}); err != nil {
		// A name collision means a previous run left this network behind; the
		// containers can still join it, so carry on.
		if errdefs.IsConflict(err) {
			return nil
		}
		return fmt.Errorf("create execution network %s: %w", name, err)
	}
	return nil
}

// RemoveExecutionNetwork stops and removes all containers for the execution,
// then removes the per-execution bridge network. Service containers may still
// be running when the execution finishes, so we force-remove them first to
// avoid leaving orphaned networks that exhaust the Docker address pool. The
// long-lived APISIX sidecar is not removed with the run: a traffic step
// attaches it to this network (AttachSidecarToExecution) so it can reach the
// run's services, and the disconnect loop below detaches it again, along with
// any container the label-based removal above missed. Docker refuses to remove
// a network with any endpoints still attached.
func RemoveExecutionNetwork(executionID string) {
	cli, ok := sharedDockerClient()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: filters.NewArgs(filters.Arg("label", "babelsuite.execution="+executionID)),
	})
	if err == nil {
		for _, c := range containers {
			cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{Force: true}) //nolint:errcheck
		}
	}

	netName := executionNetworkName(executionID)
	if info, err := cli.NetworkInspect(ctx, netName, network.InspectOptions{}); err == nil {
		for containerID := range info.Containers {
			cli.NetworkDisconnect(ctx, netName, containerID, true) //nolint:errcheck
		}
	}

	cli.NetworkRemove(ctx, netName) //nolint:errcheck
}

var (
	dockerClientOnce sync.Once
	dockerClientMu   sync.Mutex
	dockerClient     *client.Client
	dockerAvailable  bool
)

func sharedDockerClient() (*client.Client, bool) {
	dockerClientOnce.Do(func() {
		cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := cli.Ping(ctx); err != nil {
			cli.Close()
			return
		}
		dockerClientMu.Lock()
		dockerClient = cli
		dockerAvailable = true
		dockerClientMu.Unlock()
	})
	dockerClientMu.Lock()
	cli, ok := dockerClient, dockerAvailable
	dockerClientMu.Unlock()
	return cli, ok
}

// pingDocker performs a live availability check against the Docker daemon,
// re-establishing the client if it was previously unavailable.
func pingDocker(ctx context.Context) bool {
	dockerClientMu.Lock()
	cli := dockerClient
	dockerClientMu.Unlock()

	if cli == nil {
		newCli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
		if err != nil {
			return false
		}
		pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		if _, err := newCli.Ping(pingCtx); err != nil {
			newCli.Close()
			return false
		}
		dockerClientMu.Lock()
		dockerClient = newCli
		dockerAvailable = true
		dockerClientMu.Unlock()
		return true
	}

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := cli.Ping(pingCtx)
	if err != nil {
		dockerClientMu.Lock()
		dockerAvailable = false
		dockerClientMu.Unlock()
		return false
	}
	dockerClientMu.Lock()
	dockerAvailable = true
	dockerClientMu.Unlock()
	return true
}

func streamContainerLogs(ctx context.Context, cli *client.Client, containerID string, step StepSpec, emit func(logstream.Line)) {
	logStream, err := cli.ContainerLogs(ctx, containerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err != nil {
		return
	}
	defer logStream.Close()
	pr, pw := io.Pipe()
	go func() {
		stdcopy.StdCopy(pw, pw, logStream)
		pw.Close()
	}()
	scanner := bufio.NewScanner(pr)
	for scanner.Scan() {
		text := strings.TrimRight(scanner.Text(), "\r\n")
		if text != "" {
			emit(containerLine(step, text))
		}
	}
}

// buildStepScript returns a POSIX shell script that runs the node's commands
// or file. Returns empty string when neither is configured.
// typescriptCommand runs a .ts step without downloading anything when the
// image's own Node can do the job. Node strips type annotations natively from
// 22.6 on, so `npx ts-node` — which fetches ts-node from the registry on first
// use — is a network dependency a step should not need just to run TypeScript.
// That fetch is also the first thing to break behind a proxy, and it fails with
// an npm error that says nothing about the step.
//
// The probe asks the runtime whether it understands the flag rather than
// parsing a version string, and only an image too old to strip types falls back
// to the fetch.
func typescriptCommand(target string) string {
	return "if node --experimental-strip-types -e '' >/dev/null 2>&1; then\n" +
		"  node --experimental-strip-types " + target + "\n" +
		"else\n" +
		"  npx --yes ts-node " + target + "\n" +
		"fi\n"
}

func buildStepScript(step StepSpec) string {
	if step.Node.FileContent == "" && len(step.Node.Commands) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("set -e\n")
	sb.WriteString("cd " + containerWorkspaceMount + "\n")

	if step.Node.FileContent != "" {
		ext := strings.ToLower(filepath.Ext(step.Node.File))
		// .sh runs under sh, not bash. A step picks its image for the tool it
		// needs — curlimages/curl, redis, node-alpine — and most of those are
		// Alpine-based with no bash at all, so `bash script.sh` exits 127 with
		// nothing to say for itself. The script is already piped through
		// /bin/sh by the entrypoint below, so sh is the shell this runner can
		// actually promise. A script that genuinely needs bash says so by
		// being named .bash.
		interpreter := map[string]string{
			".py": "python", ".sh": "sh", ".bash": "bash",
			".js": "node", ".rb": "ruby",
		}[ext]
		target := containerWorkspaceMount + "/" + step.Node.File
		switch {
		case ext == ".ts":
			sb.WriteString(typescriptCommand(target))
		case interpreter != "":
			sb.WriteString(interpreter + " " + target + "\n")
		default:
			sb.WriteString("/bin/sh " + target + "\n")
		}
	}
	for _, cmd := range step.Node.Commands {
		sb.WriteString(strings.ReplaceAll(cmd, "\r", "") + "\n")
	}
	return sb.String()
}

// isDetachedService reports whether the step should run as a background
// container (started then left running while downstream steps proceed).
func isDetachedService(step StepSpec) bool {
	return step.Node.Kind == "service" && (step.Node.Image != "" || resolveStepImage(step) != "")
}

// buildDeviceConfig translates a profile's services.<name>.devices into Docker
// device options, per the mapping in docs/profiles.md: "gpu" is every NVIDIA
// GPU, "gpu:N" is N of them, and "/dev/<path>" is that host device. Added in
// #53 and dropped by accident in #54's refactor of this file, which left the
// documented passthrough silently ignored on Docker while the Kubernetes and
// remote backends still honoured it.
func buildDeviceConfig(devices []string) ([]container.DeviceRequest, []container.DeviceMapping) {
	var requests []container.DeviceRequest
	var mappings []container.DeviceMapping
	for _, d := range devices {
		switch {
		case d == "gpu":
			requests = append(requests, container.DeviceRequest{
				Driver:       "nvidia",
				Count:        -1,
				Capabilities: [][]string{{"gpu"}},
			})
		case strings.HasPrefix(d, "gpu:"):
			n := 1
			fmt.Sscanf(strings.TrimPrefix(d, "gpu:"), "%d", &n) //nolint:errcheck // a malformed count keeps 1
			requests = append(requests, container.DeviceRequest{
				Driver:       "nvidia",
				Count:        n,
				Capabilities: [][]string{{"gpu"}},
			})
		case strings.HasPrefix(d, "/dev/"):
			mappings = append(mappings, container.DeviceMapping{
				PathOnHost:        d,
				PathInContainer:   d,
				CgroupPermissions: "rwm",
			})
		}
	}
	return requests, mappings
}

func runInDocker(ctx context.Context, step StepSpec, emit func(logstream.Line)) error {
	cli, ok := sharedDockerClient()
	if !ok {
		return fmt.Errorf("docker daemon unavailable")
	}

	img := step.Node.Image
	if img == "" {
		img = resolveStepImage(step)
	}
	if img == "" {
		return fmt.Errorf("no image configured for step %q", step.Node.Name)
	}

	workspaceDir := ExecutionWorkspaceDir(step.ExecutionID)
	// The step container drops every capability, so the root inside it has no
	// DAC_OVERRIDE and is bound by the permission bits like any other user.
	// Nothing makes its uid match whoever runs the control plane, so an
	// owner-only workspace is one the step cannot cd into, and an owner-only
	// writable one is a step that cannot produce the artifacts it exports.
	// Both only show up where the daemon shares the host's filesystem: a
	// desktop Docker passes the mount through a VM that drops ownership, which
	// is why this surfaces on Linux alone.
	if err := os.MkdirAll(workspaceDir, 0777); err != nil {
		return fmt.Errorf("workspace dir unavailable: %w", err)
	}
	// MkdirAll is masked by the process umask, and the step has to write here
	// whatever that umask happens to be.
	if err := os.Chmod(workspaceDir, 0777); err != nil {
		return fmt.Errorf("workspace dir not writable: %w", err)
	}

	// Write file content to workspace before container starts so it is
	// available at the mounted path inside the container.
	if step.Node.FileContent != "" {
		hostPath := filepath.Join(workspaceDir, filepath.FromSlash(step.Node.File))
		if mkErr := os.MkdirAll(filepath.Dir(hostPath), 0755); mkErr == nil {
			// Normalize CRLF → LF so scripts work correctly inside Linux containers.
			content := strings.ReplaceAll(step.Node.FileContent, "\r\n", "\n")
			content = strings.ReplaceAll(content, "\r", "\n")
			_ = os.WriteFile(hostPath, []byte(content), 0644)
		}
	}

	env := make([]string, 0, len(step.Env)+3)
	for k, v := range step.Env {
		env = append(env, k+"="+v)
	}
	env = append(env, "BABELSUITE_WORKSPACE_DIR="+containerWorkspaceMount)
	// A step that calls a stateful mock names its run, so the mock keeps this
	// run's state apart from every other run of the suite.
	env = append(env, "BABELSUITE_EXECUTION_ID="+step.ExecutionID)
	// The sidecar address the step can actually reach. The traffic, plugin and
	// security executors already use step.GatewayURL, but a plain task or test
	// had no way to learn it, so a script that wanted to call a mock surface had
	// to hardcode a host and port. Exported only when there is one, so a suite
	// with no mock surfaces does not advertise an empty address.
	//
	// The address the control plane uses (127.0.0.1:<published port>) is the
	// container itself from in here. When the sidecar is on the run's network,
	// the container gets its address there instead, and the same gateway for
	// every mock; otherwise it gets the host one, which only a process on the
	// host can use.
	gatewayURL, gatewayURLs := strings.TrimSpace(step.GatewayURL), step.GatewayURLs
	if gatewayURL != "" && sidecarAttached(ctx, step) {
		inNetwork := "http://" + GatewayAlias + ":9080"
		gatewayURL = inNetwork
		gatewayURLs = make([]string, len(step.GatewayURLs))
		for i := range gatewayURLs {
			gatewayURLs[i] = inNetwork
		}
	}
	if gatewayURL != "" {
		env = append(env, "BABELSUITE_GATEWAY_URL="+gatewayURL)
		// The same port serves gRPC: the gateway speaks HTTP/2 next to HTTP/1.1.
		env = append(env, "BABELSUITE_GRPC_GATEWAY="+gatewayHostPort(gatewayURL))
		env = append(env, streamMockEnv(step, gatewayURL, sidecarAttached(ctx, step))...)
	}
	if len(gatewayURLs) > 0 {
		env = append(env, "BABELSUITE_GATEWAY_URLS="+strings.Join(gatewayURLs, ","))
	}

	containerName := fmt.Sprintf("babel-%s-%s", sanitizeID(step.ExecutionID), sanitizeID(step.Node.ID))
	cfg := &container.Config{
		Image: img,
		Env:   env,
		Labels: managedLabels(step.ExecutionID, map[string]string{
			"babelsuite.execution": step.ExecutionID,
			"babelsuite.step":      step.Node.ID,
			"babelsuite.kind":      step.Node.Kind,
			labelSuite:             step.SuiteID,
			labelProfile:           step.Profile,
		}),
	}

	if script := buildStepScript(step); script != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(script))
		cfg.Env = append(cfg.Env, "BABELSUITE_SCRIPT="+encoded)
		cfg.Entrypoint = []string{"/bin/sh", "-c", "echo $BABELSUITE_SCRIPT | base64 -d | /bin/sh -e"}
	}

	pidsLimit := containerPidsLimit
	// A step that hosts a process with a large working set — a model server, a
	// JVM — needs more than the default ceiling, and that is environment-shaped
	// rather than topology-shaped, so it comes from the profile.
	memoryLimit := int64(containerMemoryLimit)
	// Left unset, Docker allows swap up to twice the limit, so a step told it
	// has N bytes can actually occupy 2N. That is worth pinning when an
	// operator named a number and expects it honoured — but only then. Pinning
	// it on the default ceiling instead halves the budget every existing step
	// has always had, which is how payment-suite's migrate step started
	// stalling under a limit that had not visibly changed.
	memorySwapLimit := int64(0)
	if step.Node.MemoryBytes > 0 {
		if step.Node.MemoryBytes < minContainerMemoryLimit {
			return fmt.Errorf("step %q: memory limit %d bytes is below the %d bytes Docker requires",
				step.Node.Name, step.Node.MemoryBytes, int64(minContainerMemoryLimit))
		}
		memoryLimit = step.Node.MemoryBytes
		memorySwapLimit = step.Node.MemoryBytes
	}
	deviceRequests, deviceMappings := buildDeviceConfig(step.Node.Devices)
	hostCfg := &container.HostConfig{
		AutoRemove: false,
		Resources: container.Resources{
			Memory:         memoryLimit,
			MemorySwap:     memorySwapLimit,
			NanoCPUs:       step.Node.MilliCPU * 1_000_000,
			PidsLimit:      &pidsLimit,
			Devices:        deviceMappings,
			DeviceRequests: deviceRequests,
		},
		Binds: []string{workspaceDir + ":" + containerWorkspaceMount + ":rw"},
	}
	if !isDetachedService(step) {
		hostCfg.CapDrop = []string{"ALL"}
		hostCfg.SecurityOpt = []string{"no-new-privileges:true"}
	}

	if len(step.PublishPorts) > 0 {
		cfg.ExposedPorts = nat.PortSet{}
		hostCfg.PortBindings = nat.PortMap{}
		for _, p := range step.PublishPorts {
			port := nat.Port(p)
			cfg.ExposedPorts[port] = struct{}{}
			hostCfg.PortBindings[port] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}}
		}
	}

	// Every step joins the per-execution network, whatever the profile's
	// network.mode says. Container-name DNS only works on a user-defined
	// network, and the shipped modules address other steps by name
	// unconditionally — postgres connect() dials "<db name>:5432" — so making
	// this conditional silently breaks any suite that does not opt in.
	netName := executionNetworkName(step.ExecutionID)
	aliases := step.NetworkAliases
	if len(aliases) == 0 {
		alias := step.Node.Name
		if idx := strings.LastIndex(alias, "/"); idx >= 0 {
			alias = alias[idx+1:]
		}
		aliases = []string{alias}
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			netName: {Aliases: aliases},
		},
	}

	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
	if errdefs.IsNotFound(err) {
		emit(line(step, "info", fmt.Sprintf("[%s] Pulling image %s.", step.Node.Name, img)))
		pullOut, pullErr := cli.ImagePull(ctx, img, image.PullOptions{})
		if pullErr != nil {
			return fmt.Errorf("image pull failed for %s: %w", img, pullErr)
		}
		io.Copy(io.Discard, pullOut) //nolint:errcheck
		pullOut.Close()
		created, err = cli.ContainerCreate(ctx, cfg, hostCfg, netCfg, nil, containerName)
	}
	if err != nil {
		return fmt.Errorf("container create failed: %w", err)
	}

	// Service containers run for the lifetime of the execution context.
	// Don't defer removal here — a background goroutine handles cleanup.
	if !isDetachedService(step) {
		defer func() {
			rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cli.ContainerRemove(rmCtx, created.ID, container.RemoveOptions{Force: true})
		}()
	}

	if err := cli.ContainerStart(ctx, created.ID, container.StartOptions{}); err != nil {
		// A detached service has no removal defer, because the goroutine that
		// owns its lifetime is only started once it is running. A failed start
		// therefore has no owner at all, and the container is left in "created"
		// holding a reference to the execution network, which then cannot be
		// removed — enough of those and Docker runs out of address pools.
		if isDetachedService(step) {
			rmCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cli.ContainerRemove(rmCtx, created.ID, container.RemoveOptions{Force: true}) //nolint:errcheck
		}
		return fmt.Errorf("container start failed: %w", err)
	}
	emit(line(step, "info", fmt.Sprintf("[%s] Container started.", step.Node.Name)))

	// For detached services: stream logs in the background, watch for early
	// exit (crash), and clean up when the step context is cancelled.
	if isDetachedService(step) {
		go streamContainerLogs(context.Background(), cli, created.ID, step, emit)
		go func() {
			waitCh, _ := cli.ContainerWait(context.Background(), created.ID, container.WaitConditionNotRunning)
			select {
			case <-ctx.Done():
			case <-waitCh:
			}
			stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cli.ContainerStop(stopCtx, created.ID, container.StopOptions{})
			cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true})
		}()
		// Brief window to detect an immediate crash.
		crashCh, _ := cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
		select {
		case result := <-crashCh:
			return fmt.Errorf("service exited immediately with code %d", result.StatusCode)
		case <-time.After(500 * time.Millisecond):
		}
		return nil
	}

	var logWg sync.WaitGroup
	defer logWg.Wait()
	logStream, err := cli.ContainerLogs(ctx, created.ID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Timestamps: false,
	})
	if err == nil {
		logWg.Add(1)
		go func() {
			defer logWg.Done()
			defer logStream.Close()
			pr, pw := io.Pipe()
			go func() {
				stdcopy.StdCopy(pw, pw, logStream)
				pw.Close()
			}()
			scanner := bufio.NewScanner(pr)
			for scanner.Scan() {
				text := strings.TrimRight(scanner.Text(), "\r\n")
				if text != "" {
					emit(containerLine(step, text))
				}
			}
		}()
	}

	waitCh, errCh := cli.ContainerWait(ctx, created.ID, container.WaitConditionNotRunning)
	var containerRunErr error
	select {
	case <-ctx.Done():
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cli.ContainerStop(stopCtx, created.ID, container.StopOptions{})
		return context.Canceled
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("container wait error: %w", err)
		}
	case result := <-waitCh:
		if result.Error != nil && result.Error.Message != "" {
			containerRunErr = fmt.Errorf("container exited with error: %s", result.Error.Message)
		} else if result.StatusCode != 0 {
			containerRunErr = fmt.Errorf("container exited with code %d", result.StatusCode)
		}
	}

	if step.OnArtifact != nil && len(step.ArtifactExports) > 0 {
		deliverArtifacts(step, collectMountArtifacts(workspaceDir, step.ArtifactExports))
	}

	if containerRunErr != nil {
		return containerRunErr
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Container finished successfully.", step.Node.Name)))
	return nil
}

func resolveStepImage(step StepSpec) string {
	switch step.Node.Kind {
	case "task":
		return stepImageFromVariant(step.Node.Variant)
	case "test":
		return stepImageFromVariant(step.Node.Variant)
	case "service":
		return stepImageFromVariant(step.Node.Variant)
	}
	return ""
}

func stepImageFromVariant(variant string) string {
	if variant == "task.run" || variant == "test.run" {
		return "alpine:3.19"
	}
	return ""
}

// SidecarContainerName returns the stable Docker container name for the
// APISIX sidecar that serves a given suite+profile combination.
// The sidecar is long-lived — one per suite, shared across executions.
func SidecarContainerName(suiteID, profile string) string {
	slug := strings.NewReplacer(".", "-", "/", "-", " ", "-").Replace(profile)
	return "babel-sidecar-" + sanitizeID(suiteID) + "-" + sanitizeID(slug)
}

// SidecarConfDir returns the host directory where config files for the sidecar
// are written. Both config.yaml (standalone mode) and apisix.yaml (routes) live here.
func SidecarConfDir(suiteID, profile string) string {
	slug := strings.NewReplacer(".", "-", "/", "-", " ", "-").Replace(profile)
	return filepath.Join(os.TempDir(), "babel-sidecar", sanitizeID(suiteID)+"-"+sanitizeID(slug), "conf")
}

// EnsureSuiteSidecar starts the APISIX sidecar for a suite if it is not
// already running. Idempotent — safe to call before every execution.
// The sidecar is named babel-sidecar-{suiteID}-{profileSlug} and persists
// across executions until explicitly stopped or the host restarts.
func EnsureSuiteSidecar(suiteConfig apisix.SuiteConfig, profile, sidecarImage, configMountPath string) error {
	cli, ok := sharedDockerClient()
	if !ok {
		return nil // Docker not available — skip silently
	}

	suiteID := suiteConfig.ID
	containerName := SidecarContainerName(suiteID, profile)

	// Two runs of one suite that boot together both land here. Checking and then
	// creating is not atomic, so without this the slower one failed with "name
	// already in use", or removed the sidecar the faster one had just started.
	// The second caller waits, then finds the running sidecar and reuses it.
	unlock := lockSidecarStart(containerName)
	defer unlock()

	if sidecarImage == "" {
		sidecarImage = apisix.DefaultSidecarImage
	}
	warnUntestedSidecarImage(sidecarImage)
	if configMountPath == "" {
		configMountPath = "/usr/local/apisix/conf/apisix.yaml"
	}

	// Everything the sidecar reads at startup, rendered first so a running
	// sidecar can be compared with it: one started from an older configuration
	// (a fixed cannon, a changed plugin, a new route) is recreated instead of
	// being reused and silently keeping the old behaviour.
	apisixYAML := apisix.RenderStandaloneConfig(suiteConfig)
	configYAML := apisix.SidecarConfig(suiteConfig)
	streamEndpoints := apisix.StreamEndpoints(suiteConfig)
	engineAddr := sidecarEngineAddr()
	engineGRPCAddr := sidecarEngineGRPCAddr()
	mockToken := currentMockToken()
	configHash := sidecarHash(suiteConfig, sidecarImage)

	ctx2s, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx2s, containerName)
	if err == nil && info.State != nil && info.State.Running &&
		info.Config != nil && info.Config.Labels[sidecarConfigHashLabel] == configHash {
		return nil
	}
	exists := err == nil

	confDir := SidecarConfDir(suiteID, profile)
	if err := os.MkdirAll(confDir, 0755); err != nil {
		return fmt.Errorf("sidecar conf dir: %w", err)
	}

	// config.yaml (standalone mode), apisix.yaml (this suite's routes and
	// plugins) and the Lua plugin files, all bind-mounted into the container.
	luaDir := filepath.Join(confDir, "..", "plugins")
	writeFiles := func() error {
		return writeSidecarFiles(confDir, luaDir, configYAML, apisixYAML, suiteConfig.CustomPlugins)
	}
	if err := writeFiles(); err != nil {
		return err
	}

	exposedPorts := nat.PortSet{"9080/tcp": struct{}{}}
	for _, endpoint := range streamEndpoints {
		exposedPorts[nat.Port(fmt.Sprintf("%d/%s", endpoint.Port, endpoint.Transport))] = struct{}{}
	}

	containerCfg := &container.Config{
		Image:        sidecarImage,
		ExposedPorts: exposedPorts,
		Env: []string{
			"APISIX_STAND_ALONE=true",
			"BABELSUITE_ENGINE_ADDR=" + engineAddr,
			"BABELSUITE_ENGINE_GRPC_ADDR=" + engineGRPCAddr,
			"BABELSUITE_MOCK_TOKEN=" + mockToken,
		},
		Labels: sidecarLabelsWithHash(suiteID, profile, configHash),
	}
	// On Linux, host.docker.internal is not pre-seeded (unlike Mac/Windows which run
	// Docker in a VM). Adding it via host-gateway makes BABELSUITE_ENGINE_ADDR resolve
	// on all platforms — Docker replaces "host-gateway" with the actual host IP.
	extraHosts := []string{"host.docker.internal:host-gateway"}
	seen := map[string]bool{}
	for _, surface := range suiteConfig.APISurfaces {
		if u, err := url.Parse(surface.MockHost); err == nil && !brokerScheme(u.Scheme) {
			// A broker's host is where the broker is, not a name the gateway
			// answers to: mapping it to the sidecar's own loopback would point a
			// publisher at the wrong place.
			if h := strings.TrimSpace(u.Hostname()); h != "" && !seen[h] {
				// Map mock surface hostname to 127.0.0.1 so Lua plugins calling
				// e.g. spice-service.mock.internal:9080 loop back through APISIX's
				// own proxy-rewrite route (which adds the Authorization header).
				extraHosts = append(extraHosts, h+":127.0.0.1")
				seen[h] = true
			}
		}
	}

	// The host port is chosen here rather than left to Docker. A port Docker
	// picks can change when a run network becomes the container's gateway (see
	// AttachSidecarToExecution), and everything that was handed the sidecar's
	// address would then be talking to nothing. A bound port never moves.
	var hostPort string
	hostCfg := &container.HostConfig{
		AutoRemove: false,
		ExtraHosts: extraHosts,
		Binds: []string{
			// File-level mounts so APISIX can still write nginx.conf to its own conf dir.
			// config.yaml is writable: with APISIX_STAND_ALONE set, the 3.9
			// image's entrypoint rewrites an existing config.yaml in place to
			// force standalone mode, and exits on a read-only one — the image
			// the demo settings pin. The file is regenerated before every
			// start and already says standalone, so the rewrite changes nothing.
			filepath.Join(confDir, "config.yaml") + ":/usr/local/apisix/conf/config.yaml",
			filepath.Join(confDir, "apisix.yaml") + ":" + configMountPath + ":ro",
			luaDir + ":" + apisix.LuaPluginMountPath + ":ro",
			// Stream plugins load from a directory of their own.
			filepath.Join(luaDir, apisix.StreamPluginDir) + ":" + apisix.StreamLuaMountPath + ":ro",
		},
	}

	ctx30s, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	// Remove a stopped container, or one started from another configuration, so
	// its name is free. When there is none, nothing is removed: a container that
	// appears now was started by someone else a moment ago and is not ours to kill.
	if exists {
		cli.ContainerRemove(ctx30s, containerName, container.RemoveOptions{Force: true}) //nolint:errcheck
	}

	createAndStart := func() (string, error) {
		port, err := freeLoopbackPort()
		if err != nil {
			return "", fmt.Errorf("choose a host port for the sidecar: %w", err)
		}
		hostPort = port
		hostCfg.PortBindings = nat.PortMap{
			"9080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: hostPort}},
		}
		// Each TCP and UDP mock is published on a host port chosen the same way,
		// for the same reason.
		for _, endpoint := range streamEndpoints {
			streamPort, err := freeLoopbackPort()
			if err != nil {
				return "", fmt.Errorf("choose a host port for the %s mock %s: %w", endpoint.Transport, endpoint.Operation, err)
			}
			hostCfg.PortBindings[nat.Port(fmt.Sprintf("%d/%s", endpoint.Port, endpoint.Transport))] = []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: streamPort}}
		}
		created, err := cli.ContainerCreate(ctx30s, containerCfg, hostCfg, nil, nil, containerName)
		if errdefs.IsNotFound(err) {
			pullOut, pullErr := cli.ImagePull(ctx30s, sidecarImage, image.PullOptions{})
			if pullErr != nil {
				return "", fmt.Errorf("pull apisix image: %w", pullErr)
			}
			io.Copy(io.Discard, pullOut) //nolint:errcheck
			pullOut.Close()
			created, err = cli.ContainerCreate(ctx30s, containerCfg, hostCfg, nil, nil, containerName)
		}
		if err != nil {
			return "", fmt.Errorf("create sidecar container: %w", err)
		}
		if err := cli.ContainerStart(ctx30s, created.ID, container.StartOptions{}); err != nil {
			cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true}) //nolint:errcheck
			return "", fmt.Errorf("start sidecar container: %w", err)
		}
		return created.ID, nil
	}

	// On Docker Desktop the files above were written a moment ago, and the VM
	// that runs Docker may not see them yet. A bind source it cannot find is
	// created as an empty directory, and the mount of a directory onto a file
	// then fails with "not a directory" — on a suite's first start, and again on
	// every start after it, because the directory stays. Clear the directories,
	// write the files again, give the file share a moment, and try once more.
	var containerID string
	var startErr error
	for attempt := 1; attempt <= 3; attempt++ {
		containerID, startErr = createAndStart()
		if startErr == nil {
			break
		}
		// Another process took the port between choosing it and starting.
		if isPortInUseError(startErr) {
			continue
		}
		if !isMountSourceError(startErr) {
			break
		}
		time.Sleep(2 * time.Second)
		if err := writeFiles(); err != nil {
			return err
		}
	}
	if startErr != nil {
		// Another process (an agent on this host, a second control plane) made
		// the sidecar between our check and our create. If it is the one we
		// would have made, use it instead of failing the run.
		if isNameConflict(startErr) {
			if url := runningSidecarWithHash(cli, containerName, configHash); url != "" {
				return waitForSidecar(cli, containerName, url, sidecarStartupWait)
			}
		}
		return startErr
	}
	created := struct{ ID string }{ID: containerID}

	// Brief window to catch immediate crash (bad config, wrong image, etc.).
	crashCh, _ := cli.ContainerWait(ctx30s, created.ID, container.WaitConditionNotRunning)
	select {
	case result := <-crashCh:
		return fmt.Errorf("apisix sidecar crashed on startup (code %d)", result.StatusCode)
	case <-time.After(1500 * time.Millisecond):
	}

	return waitForSidecar(cli, containerName, "http://127.0.0.1:"+hostPort, sidecarStartupWait)
}

var untestedImageWarned sync.Map // image -> struct{}

// warnUntestedSidecarImage says once per image that the sidecar is not the
// version it is tested on. It is a warning, not a refusal: an operator may have
// a reason, and a newer image often works.
func warnUntestedSidecarImage(image string) {
	if image == apisix.DefaultSidecarImage {
		return
	}
	if _, seen := untestedImageWarned.LoadOrStore(image, struct{}{}); seen {
		return
	}
	slog.Warn("the APISIX sidecar image is not the tested one; mocks, load and scans are verified on "+apisix.DefaultSidecarImage,
		"image", image)
}

var sidecarStartLocks sync.Map // container name -> *sync.Mutex

// lockSidecarStart serializes starts of one sidecar in this process and returns
// the function that releases it.
func lockSidecarStart(containerName string) func() {
	lock, _ := sidecarStartLocks.LoadOrStore(containerName, &sync.Mutex{})
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

// isNameConflict reports whether a container could not be created because its
// name is taken.
func isNameConflict(err error) bool {
	return errdefs.IsConflict(err) || strings.Contains(strings.ToLower(err.Error()), "is already in use")
}

// runningSidecarWithHash returns the host address of the named container when it
// is running with the given configuration, and "" otherwise.
func runningSidecarWithHash(cli *client.Client, containerName, configHash string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil || info.State == nil || !info.State.Running || info.Config == nil ||
		info.Config.Labels[sidecarConfigHashLabel] != configHash || info.NetworkSettings == nil {
		return ""
	}
	bindings := info.NetworkSettings.Ports["9080/tcp"]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return ""
	}
	return "http://127.0.0.1:" + bindings[0].HostPort
}

// sidecarHash digests everything a sidecar of this suite starts with, so a running
// one can be told from one that needs recreating. What the sidecar proxies mocks
// to, and the secret it presents, are part of it: a different one recreates it.
// The secret enters the hash as a digest, never as itself.
func sidecarHash(suiteConfig apisix.SuiteConfig, image string) string {
	luaFiles := apisix.LuaPluginFiles(suiteConfig.CustomPlugins...)
	for name, code := range apisix.StreamLuaPluginFiles() {
		luaFiles[apisix.StreamPluginDir+"/"+name] = code
	}
	return sidecarConfigHash(
		image+" engine="+sidecarEngineAddr()+" grpc="+sidecarEngineGRPCAddr()+" token="+tokenDigest(currentMockToken()),
		apisix.SidecarConfig(suiteConfig), apisix.RenderStandaloneConfig(suiteConfig), luaFiles)
}

var (
	mockTokenMu sync.RWMutex
	mockToken   string
)

// ConfigureSidecarSecrets sets the secret every suite's sidecar presents to the
// control plane's mock engine. It is called once, when the control plane starts.
func ConfigureSidecarSecrets(token string) {
	mockTokenMu.Lock()
	mockToken = strings.TrimSpace(token)
	mockTokenMu.Unlock()
}

func currentMockToken() string {
	mockTokenMu.RLock()
	defer mockTokenMu.RUnlock()
	return mockToken
}

// tokenDigest identifies a secret without revealing it, for the sidecar's
// configuration hash (which is stored in a container label).
func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte("sidecar-token\x00" + token))
	return hex.EncodeToString(sum[:8])
}

// sidecarEngineGRPCAddr is where the sidecar reaches the control plane's gRPC
// mock front end from inside its container. BABELSUITE_ENGINE_GRPC_ADDR
// overrides the default.
func sidecarEngineGRPCAddr() string {
	if addr := strings.TrimSpace(os.Getenv("BABELSUITE_ENGINE_GRPC_ADDR")); addr != "" {
		return addr
	}
	return "host.docker.internal:" + DefaultGRPCMockPort
}

// DefaultGRPCMockPort is the port of the gRPC mock front end unless
// BABELSUITE_GRPC_MOCK_PORT says otherwise.
const DefaultGRPCMockPort = "8095"

// sidecarEngineAddr is where the sidecar reaches the control plane's mock
// engine from inside its container. BABELSUITE_ENGINE_ADDR overrides the
// default, for a control plane that does not listen on 8090.
func sidecarEngineAddr() string {
	if addr := strings.TrimSpace(os.Getenv("BABELSUITE_ENGINE_ADDR")); addr != "" {
		return addr
	}
	return "host.docker.internal:8090"
}

// sidecarStartupWait is how long a freshly started sidecar gets to begin
// serving. A cold container on a busy host takes several seconds, and its
// published port refuses connections until the gateway is listening.
const sidecarStartupWait = 45 * time.Second

// waitForSidecar returns once the gateway answers HTTP on base, so every step
// that uses the sidecar can rely on it serving. Any answer counts, including a
// 404 for a path no route serves. A container that stopped, or never answered,
// is an error that carries its last log lines.
func waitForSidecar(cli *client.Client, containerName, base string, within time.Duration) error {
	httpClient := &http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(within)
	var last error
	for {
		resp, err := httpClient.Get(base + "/")
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
			resp.Body.Close()
			return nil
		}
		last = err

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		info, inspectErr := cli.ContainerInspect(ctx, containerName)
		cancel()
		if inspectErr == nil && info.State != nil && !info.State.Running {
			return fmt.Errorf("apisix sidecar stopped during startup (code %d)%s", info.State.ExitCode, sidecarLogTail(cli, containerName))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("apisix sidecar did not start serving at %s within %s: %w%s", base, within, last, sidecarLogTail(cli, containerName))
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// sidecarLogTail returns the last lines the sidecar logged, ready to append to
// an error, or "" when there are none to read.
func sidecarLogTail(cli *client.Client, containerName string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	reader, err := cli.ContainerLogs(ctx, containerName, container.LogsOptions{ShowStdout: true, ShowStderr: true, Tail: "15"})
	if err != nil {
		return ""
	}
	defer reader.Close()
	var out bytes.Buffer
	_, _ = stdcopy.StdCopy(&out, &out, reader)
	if text := strings.TrimSpace(out.String()); text != "" {
		return "\n" + text
	}
	return ""
}

// freeLoopbackPort asks the OS for a TCP port that is free on 127.0.0.1.
func freeLoopbackPort() (string, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	defer listener.Close()
	_, port, err := net.SplitHostPort(listener.Addr().String())
	return port, err
}

func isPortInUseError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "port is already allocated") || strings.Contains(text, "address already in use")
}

// writeSidecarFiles writes everything the sidecar mounts. A path that is a
// directory where a file belongs (left there by a failed first start, see
// EnsureSuiteSidecar) is removed first, so it can never block a later start.
func writeSidecarFiles(confDir, luaDir, configYAML, apisixYAML string, plugins []apisix.CustomPluginConfig) error {
	configPath := filepath.Join(confDir, "config.yaml")
	routesPath := filepath.Join(confDir, "apisix.yaml")
	for _, path := range []string{configPath, routesPath} {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if err := os.RemoveAll(path); err != nil {
				return fmt.Errorf("clear %s, which is a directory: %w", path, err)
			}
		}
	}
	if err := os.MkdirAll(confDir, 0755); err != nil {
		return fmt.Errorf("sidecar conf dir: %w", err)
	}
	if err := writeSidecarConfig(configPath, configYAML); err != nil {
		return fmt.Errorf("write config.yaml: %w", err)
	}
	if err := writeFileSynced(routesPath, []byte(apisixYAML), 0644); err != nil {
		return fmt.Errorf("write apisix.yaml: %w", err)
	}
	if err := os.MkdirAll(luaDir, 0755); err != nil {
		return fmt.Errorf("create lua dir: %w", err)
	}
	if err := apisix.WriteLuaPluginFiles(luaDir, plugins...); err != nil {
		return fmt.Errorf("write lua plugins: %w", err)
	}
	return nil
}

// isMountSourceError reports whether a container start failed because a bind
// mount's source was not what Docker expected (a directory where a file belongs).
func isMountSourceError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "not a directory") || strings.Contains(text, "error mounting")
}

// writeSidecarConfig writes the sidecar's config.yaml so the container can
// rewrite it. The APISIX image runs as the unprivileged user apisix (uid 636),
// and with APISIX_STAND_ALONE set its entrypoint rewrites an existing
// config.yaml in place. On a Linux host a bind-mounted file keeps the owner and
// mode it was created with, so a file written 0644 by the control plane's user
// is read-only to uid 636: the entrypoint fails and the sidecar exits with
// code 1 before serving anything. Docker Desktop hides this by presenting bind
// mounts as world-writable. The mode is set with Chmod after the write because
// the process umask would otherwise strip the group and other write bits from
// the mode passed to WriteFile.
func writeSidecarConfig(path, content string) error {
	if err := writeFileSynced(path, []byte(content), 0o644); err != nil {
		return err
	}
	return os.Chmod(path, 0o666)
}

// writeFileSynced writes a file and flushes it to disk before returning, so a
// container started right after finds the content: Docker may see the file
// through a VM's file share that has not caught up with a write still in the
// host's cache, and a bind source it cannot find is created as an empty
// directory.
func writeFileSynced(path string, data []byte, perm os.FileMode) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// SuiteSidecarURL returns the host-accessible URL for the running APISIX sidecar
// of a suite. Returns "" when the sidecar is not running.
func SuiteSidecarURL(suiteID, profile string) string {
	port := SuiteSidecarHostPort(suiteID, profile, "9080/tcp")
	if port == "" {
		return ""
	}
	return "http://127.0.0.1:" + port
}

// SuiteSidecarHostPort returns the host port the sidecar publishes a container
// port on (for example "9100/tcp"), or "" when it is not published.
func SuiteSidecarHostPort(suiteID, profile, containerPort string) string {
	cli, ok := sharedDockerClient()
	if !ok {
		return ""
	}
	containerName := SidecarContainerName(suiteID, profile)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil || info.NetworkSettings == nil {
		return ""
	}
	bindings := info.NetworkSettings.Ports[nat.Port(containerPort)]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return ""
	}
	return bindings[0].HostPort
}

// ContainerHostPort returns the host-bound port for a given container port on
// a per-execution node's container (e.g. "8082/tcp"), so the shared APISIX
// sidecar — which never joins per-execution Docker networks, see
// RemoveExecutionNetwork — can reach it via host.docker.internal instead.
// Returns "" when the container isn't running or that port wasn't published.
func ContainerHostPort(executionID, nodeID, containerPort string) string {
	cli, ok := sharedDockerClient()
	if !ok {
		return ""
	}
	containerName := fmt.Sprintf("babel-%s-%s", sanitizeID(executionID), sanitizeID(nodeID))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return ""
	}
	bindings := info.NetworkSettings.Ports[nat.Port(containerPort)]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return ""
	}
	return bindings[0].HostPort
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, ch := range id {
		if (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9') || ch == '-' {
			b.WriteRune(ch)
		} else if ch >= 'A' && ch <= 'Z' {
			b.WriteRune(ch + 32)
		} else {
			b.WriteRune('-')
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.Trim(s, "-")
}

// brokerScheme reports whether a surface's host URL names a message broker.
func brokerScheme(scheme string) bool {
	switch strings.ToLower(strings.TrimSpace(scheme)) {
	case "kafka", "rocketmq", "mqtt", "amqp", "nats":
		return true
	}
	return false
}
