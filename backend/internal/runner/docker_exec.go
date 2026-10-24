package runner

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path"
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
	maxArtifactBytes        = 10 * 1024 * 1024  // 10 MB per artifact file
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
// long-lived APISIX sidecar never joins this network (see EnsureSuiteSidecar
// and SuiteSidecarURL — it is reached via its host-published port instead),
// so the disconnect loop below is just a defensive fallback for any container
// that the label-based removal above missed; Docker refuses to remove a
// network with any endpoints still attached.
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
	// The sidecar address the step can actually reach. The traffic, plugin and
	// security executors already use step.GatewayURL, but a plain task or test
	// had no way to learn it, so a script that wanted to call a mock surface had
	// to hardcode a host and port. Exported only when there is one, so a suite
	// with no mock surfaces does not advertise an empty address.
	if gateway := strings.TrimSpace(step.GatewayURL); gateway != "" {
		env = append(env, "BABELSUITE_GATEWAY_URL="+gateway)
	}
	if len(step.GatewayURLs) > 0 {
		env = append(env, "BABELSUITE_GATEWAY_URLS="+strings.Join(step.GatewayURLs, ","))
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
		exitStatus := "success"
		if containerRunErr != nil {
			exitStatus = "failure"
		}
		for _, export := range step.ArtifactExports {
			if !artifactTriggerMatchesStatus(export.On, exitStatus) {
				continue
			}
			content, err := readArtifactFromMount(workspaceDir, export.Path)
			if err == nil && len(content) > 0 {
				step.OnArtifact(export.Path, content)
			}
		}
	}

	if containerRunErr != nil {
		return containerRunErr
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Container finished successfully.", step.Node.Name)))
	return nil
}

// readArtifactFromMount reads an artifact file from the host-side mount directory.
// The export path is cleaned and verified to stay within the mount root to
// prevent any path traversal. Glob patterns (*, ?, [) are expanded; the
// first matching file's content is returned.
func readArtifactFromMount(mountDir, exportPath string) ([]byte, error) {
	exportPath = strings.TrimSpace(exportPath)
	if strings.ContainsAny(exportPath, "*?[") {
		return readArtifactGlob(mountDir, exportPath)
	}

	cleaned := path.Clean("/" + exportPath)
	hostPath := filepath.Join(mountDir, filepath.FromSlash(cleaned))

	// Reject any path that escapes the mount directory.
	if !strings.HasPrefix(hostPath+string(filepath.Separator), mountDir+string(filepath.Separator)) {
		return nil, fmt.Errorf("artifact path %q escapes mount directory", exportPath)
	}

	f, err := os.Open(hostPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(io.LimitReader(f, maxArtifactBytes))
}

// readArtifactGlob expands a glob export path within the mount directory and
// returns the content of the first matching file. All matches are verified to
// remain within mountDir before any file is opened.
func readArtifactGlob(mountDir, exportPath string) ([]byte, error) {
	pattern := filepath.Join(mountDir, filepath.FromSlash(path.Clean("/"+exportPath)))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("artifact glob %q: %w", exportPath, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("artifact glob %q: no matching files in mount", exportPath)
	}

	prefix := mountDir + string(filepath.Separator)
	for _, m := range matches {
		if !strings.HasPrefix(m+string(filepath.Separator), prefix) {
			return nil, fmt.Errorf("artifact glob match %q escapes mount directory", m)
		}
	}

	f, err := os.Open(matches[0])
	if err != nil {
		return nil, err
	}
	defer f.Close()

	return io.ReadAll(io.LimitReader(f, maxArtifactBytes))
}

func artifactTriggerMatchesStatus(trigger, status string) bool {
	switch strings.TrimSpace(trigger) {
	case "", "success":
		return status == "success"
	case "failure":
		return status == "failure"
	case "always":
		return true
	default:
		return false
	}
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

	// Already running — nothing to do.
	ctx2s, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx2s, containerName)
	if err == nil && info.State != nil && info.State.Running {
		return nil
	}

	confDir := SidecarConfDir(suiteID, profile)
	if err := os.MkdirAll(confDir, 0755); err != nil {
		return fmt.Errorf("sidecar conf dir: %w", err)
	}

	// config.yaml — override the image default (traditional/etcd) with standalone/yaml mode.
	const standaloneCfg = "deployment:\n  role: data_plane\n  role_data_plane:\n    config_provider: yaml\n"
	if err := os.WriteFile(filepath.Join(confDir, "config.yaml"), []byte(standaloneCfg), 0644); err != nil {
		return fmt.Errorf("write config.yaml: %w", err)
	}

	// apisix.yaml — routes + plugins for this suite.
	apisixYAML := apisix.RenderStandaloneConfig(suiteConfig)
	if err := os.WriteFile(filepath.Join(confDir, "apisix.yaml"), []byte(apisixYAML), 0644); err != nil {
		return fmt.Errorf("write apisix.yaml: %w", err)
	}

	// Lua plugin files bind-mounted alongside the config.
	luaDir := filepath.Join(confDir, "..", "plugins")
	if err := os.MkdirAll(luaDir, 0755); err != nil {
		return fmt.Errorf("create lua dir: %w", err)
	}
	if err := apisix.WriteLuaPluginFiles(luaDir, suiteConfig.CustomPlugins...); err != nil {
		return fmt.Errorf("write lua plugins: %w", err)
	}

	if sidecarImage == "" {
		sidecarImage = "apache/apisix:latest"
	}
	if configMountPath == "" {
		configMountPath = "/usr/local/apisix/conf/apisix.yaml"
	}

	portBindings := nat.PortMap{
		"9080/tcp": []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: ""}},
	}
	exposedPorts := nat.PortSet{"9080/tcp": struct{}{}}

	containerCfg := &container.Config{
		Image:        sidecarImage,
		ExposedPorts: exposedPorts,
		Env: []string{
			"APISIX_STAND_ALONE=true",
			"BABELSUITE_ENGINE_ADDR=host.docker.internal:8090",
		},
		Labels: sidecarLabels(suiteID, profile),
	}
	// On Linux, host.docker.internal is not pre-seeded (unlike Mac/Windows which run
	// Docker in a VM). Adding it via host-gateway makes BABELSUITE_ENGINE_ADDR resolve
	// on all platforms — Docker replaces "host-gateway" with the actual host IP.
	extraHosts := []string{"host.docker.internal:host-gateway"}
	seen := map[string]bool{}
	for _, surface := range suiteConfig.APISurfaces {
		if u, err := url.Parse(surface.MockHost); err == nil {
			if h := strings.TrimSpace(u.Host); h != "" && !seen[h] {
				// Map mock surface hostname to 127.0.0.1 so Lua plugins calling
				// e.g. spice-service.mock.internal:9080 loop back through APISIX's
				// own proxy-rewrite route (which adds the Authorization header).
				extraHosts = append(extraHosts, h+":127.0.0.1")
				seen[h] = true
			}
		}
	}

	hostCfg := &container.HostConfig{
		AutoRemove:   false,
		PortBindings: portBindings,
		ExtraHosts:   extraHosts,
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
		},
	}

	ctx30s, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel2()

	// Remove any stopped container with the same name before recreating.
	cli.ContainerRemove(ctx30s, containerName, container.RemoveOptions{Force: true}) //nolint:errcheck

	created, err := cli.ContainerCreate(ctx30s, containerCfg, hostCfg, nil, nil, containerName)
	if errdefs.IsNotFound(err) {
		pullOut, pullErr := cli.ImagePull(ctx30s, sidecarImage, image.PullOptions{})
		if pullErr != nil {
			return fmt.Errorf("pull apisix image: %w", pullErr)
		}
		io.Copy(io.Discard, pullOut) //nolint:errcheck
		pullOut.Close()
		created, err = cli.ContainerCreate(ctx30s, containerCfg, hostCfg, nil, nil, containerName)
	}
	if err != nil {
		return fmt.Errorf("create sidecar container: %w", err)
	}

	if err := cli.ContainerStart(ctx30s, created.ID, container.StartOptions{}); err != nil {
		cli.ContainerRemove(context.Background(), created.ID, container.RemoveOptions{Force: true}) //nolint:errcheck
		return fmt.Errorf("start sidecar container: %w", err)
	}

	// Brief window to catch immediate crash (bad config, wrong image, etc.).
	crashCh, _ := cli.ContainerWait(ctx30s, created.ID, container.WaitConditionNotRunning)
	select {
	case result := <-crashCh:
		return fmt.Errorf("apisix sidecar crashed on startup (code %d)", result.StatusCode)
	case <-time.After(1500 * time.Millisecond):
	}

	return nil
}

// SuiteSidecarURL returns the host-accessible URL for the running APISIX sidecar
// of a suite. Returns "" when the sidecar is not running.
func SuiteSidecarURL(suiteID, profile string) string {
	cli, ok := sharedDockerClient()
	if !ok {
		return ""
	}
	containerName := SidecarContainerName(suiteID, profile)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	info, err := cli.ContainerInspect(ctx, containerName)
	if err != nil {
		return ""
	}
	bindings := info.NetworkSettings.Ports["9080/tcp"]
	if len(bindings) == 0 || bindings[0].HostPort == "" {
		return ""
	}
	return "http://127.0.0.1:" + bindings[0].HostPort
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
