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

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
)

const (
	containerWorkspaceMount = "/babelsuite/workspace"
	maxArtifactBytes        = 10 * 1024 * 1024  // 10 MB per artifact file
	containerMemoryLimit    = 512 * 1024 * 1024 // 512 MB per step container
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

// ensureExecutionNetwork returns the network name for an execution.
// The network is created upfront by SetupExecutionNetwork before any tasks run.
func ensureExecutionNetwork(_ context.Context, _ *client.Client, executionID string) string {
	return executionNetworkName(executionID)
}

// SetupExecutionNetwork creates the per-execution bridge network once, before
// any container goroutines start. Call this before enqueuing tasks so that
// every concurrent runInDocker call finds the network already present.
func SetupExecutionNetwork(executionID string) {
	cli, ok := sharedDockerClient()
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	name := executionNetworkName(executionID)
	cli.NetworkCreate(ctx, name, network.CreateOptions{Driver: "bridge"}) //nolint:errcheck
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
func buildStepScript(step StepSpec) string {
	if step.Node.FileContent == "" && len(step.Node.Commands) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("set -e\n")
	sb.WriteString("cd " + containerWorkspaceMount + "\n")

	if step.Node.FileContent != "" {
		ext := strings.ToLower(filepath.Ext(step.Node.File))
		interpreter := map[string]string{
			".py": "python", ".sh": "bash", ".bash": "bash",
			".js": "node", ".rb": "ruby", ".ts": "npx ts-node",
		}[ext]
		if interpreter == "" {
			interpreter = "/bin/sh"
		}
		sb.WriteString(interpreter + " " + containerWorkspaceMount + "/" + step.Node.File + "\n")
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
	if err := os.MkdirAll(workspaceDir, 0700); err != nil {
		return fmt.Errorf("workspace dir unavailable: %w", err)
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

	env := make([]string, 0, len(step.Env)+1)
	for k, v := range step.Env {
		env = append(env, k+"="+v)
	}
	env = append(env, "BABELSUITE_WORKSPACE_DIR="+containerWorkspaceMount)

	containerName := fmt.Sprintf("babel-%s-%s", sanitizeID(step.ExecutionID), sanitizeID(step.Node.ID))
	cfg := &container.Config{
		Image: img,
		Env:   env,
		Labels: map[string]string{
			"babelsuite.execution": step.ExecutionID,
			"babelsuite.step":      step.Node.ID,
			"babelsuite.kind":      step.Node.Kind,
		},
	}

	if script := buildStepScript(step); script != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(script))
		cfg.Env = append(cfg.Env, "BABELSUITE_SCRIPT="+encoded)
		cfg.Entrypoint = []string{"/bin/sh", "-c", "echo $BABELSUITE_SCRIPT | base64 -d | /bin/sh -e"}
	}

	pidsLimit := containerPidsLimit
	hostCfg := &container.HostConfig{
		AutoRemove: false,
		Resources: container.Resources{
			Memory:    containerMemoryLimit,
			PidsLimit: &pidsLimit,
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

	netName := ensureExecutionNetwork(ctx, cli, step.ExecutionID)
	alias := step.Node.Name
	if idx := strings.LastIndex(alias, "/"); idx >= 0 {
		alias = alias[idx+1:]
	}
	netCfg := &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			netName: {Aliases: []string{alias}},
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
		Labels: map[string]string{
			"babelsuite.suite":   suiteID,
			"babelsuite.profile": profile,
			"babelsuite.kind":    "apisix-sidecar",
		},
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
			filepath.Join(confDir, "config.yaml") + ":/usr/local/apisix/conf/config.yaml:ro",
			filepath.Join(confDir, "apisix.yaml") + ":/usr/local/apisix/conf/apisix.yaml:ro",
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
