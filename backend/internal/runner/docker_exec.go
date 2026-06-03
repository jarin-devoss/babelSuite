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

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
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
	var sb strings.Builder
	sb.WriteString("set -e\n")
	sb.WriteString("cd " + containerWorkspaceMount + "\n")

	if step.Node.FileContent != "" {
		ext := strings.ToLower(filepath.Ext(step.Node.File))
		interpreter := map[string]string{
			".py": "python", ".sh": "/bin/sh", ".bash": "bash",
			".js": "node", ".rb": "ruby", ".ts": "npx ts-node",
		}[ext]
		if interpreter == "" {
			interpreter = "/bin/sh"
		}
		sb.WriteString(interpreter + " " + containerWorkspaceMount + "/" + step.Node.File + "\n")
	}
	for _, cmd := range step.Node.Commands {
		sb.WriteString(cmd + "\n")
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
			_ = os.WriteFile(hostPath, []byte(step.Node.FileContent), 0644)
		}
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Pulling image %s.", step.Node.Name, img)))
	pullOut, err := cli.ImagePull(ctx, img, image.PullOptions{})
	if err != nil {
		return fmt.Errorf("image pull failed for %s: %w", img, err)
	}
	if _, err := io.Copy(io.Discard, pullOut); err != nil {
		pullOut.Close()
		return fmt.Errorf("image pull stream error for %s: %w", img, err)
	}
	pullOut.Close()

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

	// Inject commands or file as a base64-encoded script executed by the
	// container's shell, overriding the default image entrypoint.
	if script := buildStepScript(step); script != "" {
		encoded := base64.StdEncoding.EncodeToString([]byte(script))
		cfg.Env = append(cfg.Env, "BABELSUITE_SCRIPT="+encoded)
		cfg.Entrypoint = []string{"/bin/sh", "-c", "echo $BABELSUITE_SCRIPT | base64 -d | /bin/sh -e"}
	}

	pidsLimit := containerPidsLimit
	hostCfg := &container.HostConfig{
		AutoRemove:  false,
		CapDrop:     []string{"ALL"},
		SecurityOpt: []string{"no-new-privileges:true"},
		Resources: container.Resources{
			Memory:    containerMemoryLimit,
			PidsLimit: &pidsLimit,
		},
		Binds: []string{workspaceDir + ":" + containerWorkspaceMount + ":rw"},
	}

	emit(line(step, "info", fmt.Sprintf("[%s] Creating container %s.", step.Node.Name, containerName)))
	created, err := cli.ContainerCreate(ctx, cfg, hostCfg, nil, nil, containerName)
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
		return stepImageFromVariant(step.Node.Variant, "task")
	case "test":
		return stepImageFromVariant(step.Node.Variant, "test")
	case "service":
		return stepImageFromVariant(step.Node.Variant, "service")
	case "mock":
		return "wiremock/wiremock:3.10"
	}
	return ""
}

func stepImageFromVariant(variant, _ string) string {
	switch variant {
	case "task.run", "test.run":
		return "alpine:3.19"
	case "service.wiremock":
		return "wiremock/wiremock:3.10"
	case "service.prism":
		return "stoplight/prism:5"
	}
	return ""
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
