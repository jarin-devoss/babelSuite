package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/babelsuite/babelsuite/internal/agent"
	"github.com/babelsuite/babelsuite/internal/envloader"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
	"github.com/babelsuite/babelsuite/internal/telemetry"
)

func main() {
	envloader.Load(".env", "../.env", "../../.env")

	telPipeline, err := telemetry.Start(context.Background())
	if err != nil {
		log.Printf("telemetry: %v", err)
	}
	telemetry.InitDefaultLogger(telemetry.NewLogger(telPipeline))
	defer telPipeline.Shutdown(context.Background())

	agentID := firstNonEmpty(strings.TrimSpace(os.Getenv("AGENT_ID")), hostnameOr("worker"))
	agentName := firstNonEmpty(strings.TrimSpace(os.Getenv("AGENT_NAME")), "BabelSuite Worker")
	port := firstNonEmpty(strings.TrimSpace(os.Getenv("AGENT_PORT")), "8091")
	publicURL := firstNonEmpty(strings.TrimSpace(os.Getenv("AGENT_PUBLIC_URL")), "http://127.0.0.1:"+port)
	controlPlaneURL := strings.TrimSpace(os.Getenv("AGENT_CONTROL_PLANE_URL"))
	heartbeatInterval := durationOr("AGENT_HEARTBEAT_INTERVAL", 15*time.Second)
	pollInterval := durationOr("AGENT_POLL_INTERVAL", time.Second)
	// How many steps this agent runs at once. It is declared by the agent,
	// which knows its host, and the control plane schedules up to it.
	maxJobs := intOr("AGENT_MAX_JOBS", 1)
	// The host this agent reaches the control plane on — 10.0.2.2 from a
	// VirtualBox VM — is also where the suite sidecar publishes its port.
	controlPlaneHost := ""
	if parsed, err := url.Parse(strings.TrimSpace(os.Getenv("AGENT_CONTROL_PLANE_URL"))); err == nil {
		controlPlaneHost = parsed.Hostname()
	}

	executions := newExecutionScopes()

	localBackend := runner.NewLocal(runner.BackendConfig{
		ID:    "worker-local",
		Label: "Worker Local",
		Kind:  "local",
	})

	service := agent.NewService(agent.Info{
		AgentID:      agentID,
		Name:         agentName,
		HostURL:      publicURL,
		Status:       "ready",
		Capabilities: []string{"service", "task", "test", "traffic", "suite"},
	}, agent.ExecutorFunc(func(ctx context.Context, request agent.StepRequest, emit func(logstream.Line)) error {
		// The execution ended on the control plane: remove what it left on
		// this host, detached services and its network included.
		if request.Node.Kind == agent.ReleaseKind {
			return destroyExecution(executions, request.ExecutionID)
		}
		// The control plane creates the execution network on its own host
		// only for local runs; steps here run on this host's Docker and
		// join a network by the execution's name, so it is set up here.
		// Idempotent, so concurrent steps of one execution are safe.
		if err := runner.SetupExecutionNetwork(request.ExecutionID); err != nil {
			return err
		}
		executions.track(request.ExecutionID)
		// A service's container lives as long as the context it was started
		// with. The job's context ends once the service is up, which stopped
		// it a second later — kafka never served the steps after it. On the
		// local backend that context is the execution's, so here it is too:
		// it lasts until the release job for the execution.
		if request.Node.Kind == "service" {
			ctx = executions.context(request.ExecutionID)
		}
		return localBackend.Run(ctx, runner.StepSpec{
			ExecutionID:      request.ExecutionID,
			SuiteID:          request.SuiteID,
			SuiteTitle:       request.SuiteTitle,
			SuiteRepository:  request.SuiteRepository,
			Profile:          request.Profile,
			RuntimeProfile:   request.RuntimeProfile,
			Env:              cloneStringMap(request.Env),
			Headers:          cloneStringMap(request.Headers),
			SourceSuiteID:    request.SourceSuiteID,
			SourceSuiteTitle: request.SourceSuiteTitle,
			SourceRepository: request.SourceRepository,
			SourceVersion:    request.SourceVersion,
			ResolvedRef:      request.ResolvedRef,
			Digest:           request.Digest,
			DependencyAlias:  request.DependencyAlias,
			Load:             cloneLoadSpec(request.Load),
			Evaluation:       cloneStepEvaluation(request.Evaluation),
			OnFailure:        append([]string{}, request.OnFailure...),
			ArtifactExports:  cloneArtifactExports(request.ArtifactExports),
			GatewayURL:       runner.RewriteLoopbackURL(request.GatewayURL, controlPlaneHost),
			GatewayURLs:      rewriteAll(request.GatewayURLs, controlPlaneHost),
			NetworkAliases:   append([]string{}, request.NetworkAliases...),
			Node: runner.StepNode{
				ID:          request.Node.ID,
				Name:        request.Node.Name,
				Kind:        request.Node.Kind,
				Variant:     request.Node.Variant,
				DependsOn:   append([]string{}, request.Node.DependsOn...),
				Image:       request.Node.Image,
				File:        request.Node.File,
				Commands:    append([]string{}, request.Node.Commands...),
				FileContent: request.Node.FileContent,
				Message:     request.Node.Message,
				Devices:     append([]string{}, request.Node.Devices...),
				MemoryBytes: request.Node.MemoryBytes,
				MilliCPU:    request.Node.MilliCPU,
			},
		}, emit)
	}))

	// The control plane's Environments page cannot see this host's Docker, so
	// the agent reports what it holds and reaps on request — the same cleanup
	// the release job and shutdown run.
	service.ConfigureInventory(
		func(ctx context.Context) (any, error) { return runner.DockerInventory(ctx) },
		func(_ context.Context, executionID string) error { return destroyExecution(executions, executionID) },
	)

	agentSecret := strings.TrimSpace(os.Getenv("AGENT_SHARED_SECRET"))

	server := &http.Server{
		Addr:    withPortPrefix(port),
		Handler: agent.NewHandler(service, agentSecret),
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	controlPlane := agent.NewControlPlaneClient(controlPlaneURL, nil, agentSecret)
	if controlPlaneURL != "" {
		register := func(ctx context.Context) error {
			registerCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			return controlPlane.Register(registerCtx, agent.RegisterRequest{
				AgentID:      agentID,
				Name:         agentName,
				HostURL:      publicURL,
				Capabilities: []string{"service", "task", "test", "traffic", "suite"},
				Capacity:     maxJobs,
			})
		}
		registered := make(chan struct{})
		go registerAndHeartbeat(rootCtx, controlPlane, agentID, heartbeatInterval, register, func() { close(registered) })
		// One claim loop per slot: each claims a step, runs it and claims the
		// next, so the agent never holds more than maxJobs steps. They start
		// once the agent is registered, so the control plane schedules
		// against the capacity the agent declared rather than a stale record.
		worker := agent.NewWorker(agentID, pollInterval, controlPlane, service)
		var workers sync.WaitGroup
		go func() {
			select {
			case <-rootCtx.Done():
				return
			case <-registered:
			}
			for range maxJobs {
				workers.Go(func() {
					if err := worker.Run(rootCtx); err != nil && err != context.Canceled {
						slog.Error("agent worker loop stopped", "error", err)
					}
				})
			}
			slog.Info("agent claiming work", "agentID", agentID, "maxJobs", maxJobs)
		}()
		// On shutdown, destroy what this agent still holds once its steps
		// have stopped. Nothing else would: the control plane's release job
		// for these executions has no agent left to claim it.
		defer func() {
			stopped := make(chan struct{})
			go func() { workers.Wait(); close(stopped) }()
			select {
			case <-stopped:
			case <-time.After(15 * time.Second):
			}
			for _, executionID := range executions.releaseAll() {
				_ = destroyExecution(executions, executionID)
				slog.Info("destroyed execution on shutdown", "executionID", executionID)
			}
		}()
		defer func() {
			unregisterCtx, unregisterCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer unregisterCancel()
			if err := controlPlane.Unregister(unregisterCtx, agentID); err != nil {
				slog.Error("agent unregister failed", "error", err)
			}
		}()
	}

	go func() {
		<-rootCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	slog.Info("babelsuite agent listening", "addr", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("agent server error", "error", err)
		os.Exit(1)
	}
}

// registerAndHeartbeat registers the agent, retrying until the control plane
// takes it, then heartbeats. A failed heartbeat registers again: a control
// plane that was down when the agent started — or restarted since — otherwise
// keeps the record it had, with the capacity the agent declared last time, or
// none, and the agent's slots never reach the scheduler.
func registerAndHeartbeat(ctx context.Context, client *agent.ControlPlaneClient, agentID string, interval time.Duration, register func(context.Context) error, onFirstRegistration func()) {
	registered := false
	first := true
	tryRegister := func() {
		if err := register(ctx); err != nil {
			slog.Error("agent register failed; retrying", "error", err)
			return
		}
		registered = true
		if first {
			first = false
			if onFirstRegistration != nil {
				onFirstRegistration()
			}
		}
	}
	tryRegister()

	retry := min(interval, 3*time.Second)
	for {
		wait := interval
		if !registered {
			wait = retry
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		if !registered {
			tryRegister()
			continue
		}
		heartbeatCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		err := client.Heartbeat(heartbeatCtx, agentID)
		cancel()
		if err != nil {
			slog.Error("agent heartbeat failed; registering again", "error", err)
			registered = false
			tryRegister()
		}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil {
		return fallback
	}
	return name
}

func withPortPrefix(port string) string {
	if strings.Contains(port, ":") {
		return port
	}
	return ":" + port
}

// destroyExecution removes what an execution left on this host: its services
// first, by ending their scope, then its containers and network, then its
// workspace. The release job, shutdown and a reap from the Environments page
// all end an execution this way.
func destroyExecution(executions *executionScopes, executionID string) error {
	executions.release(executionID)
	runner.RemoveExecutionNetwork(executionID)
	return os.RemoveAll(runner.ExecutionWorkspaceDir(executionID))
}

// executionScopes holds the executions this agent has run steps for, each
// with a context that outlives its steps, for the services that must keep
// running after the job that started them.
type executionScopes struct {
	mu      sync.Mutex
	cancels map[string]context.CancelFunc
	ctxs    map[string]context.Context
}

func newExecutionScopes() *executionScopes {
	return &executionScopes{cancels: map[string]context.CancelFunc{}, ctxs: map[string]context.Context{}}
}

// track records that the execution has left something on this host.
func (s *executionScopes) track(executionID string) {
	s.context(executionID)
}

func (s *executionScopes) context(executionID string) context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx, ok := s.ctxs[executionID]; ok {
		return ctx
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.ctxs[executionID], s.cancels[executionID] = ctx, cancel
	return ctx
}

func (s *executionScopes) release(executionID string) {
	s.mu.Lock()
	cancel := s.cancels[executionID]
	delete(s.cancels, executionID)
	delete(s.ctxs, executionID)
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// releaseAll ends every scope and returns the executions they belonged to.
func (s *executionScopes) releaseAll() []string {
	s.mu.Lock()
	ids := make([]string, 0, len(s.cancels))
	for id := range s.cancels {
		ids = append(ids, id)
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.release(id)
	}
	return ids
}

// rewriteAll applies runner.RewriteLoopbackURL to every URL.
func rewriteAll(urls []string, host string) []string {
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = runner.RewriteLoopbackURL(u, host)
	}
	return out
}

// intOr reads a positive integer from the environment, or returns fallback.
func intOr(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func durationOr(key string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func cloneArtifactExports(input []agent.ArtifactExport) []runner.ArtifactExport {
	if len(input) == 0 {
		return nil
	}

	output := make([]runner.ArtifactExport, len(input))
	for index, item := range input {
		output[index] = runner.ArtifactExport{
			Path:   item.Path,
			Name:   item.Name,
			On:     item.On,
			Format: item.Format,
		}
	}
	return output
}

func cloneLoadSpec(input *suites.LoadSpec) *suites.LoadSpec {
	if input == nil {
		return nil
	}

	output := *input
	output.Users = make([]suites.LoadUser, len(input.Users))
	for userIndex, user := range input.Users {
		output.Users[userIndex] = user
		output.Users[userIndex].Tasks = make([]suites.LoadTask, len(user.Tasks))
		for taskIndex, task := range user.Tasks {
			output.Users[userIndex].Tasks[taskIndex] = task
			output.Users[userIndex].Tasks[taskIndex].Checks = append([]suites.LoadThreshold{}, task.Checks...)
			output.Users[userIndex].Tasks[taskIndex].Request.Headers = cloneStringMap(task.Request.Headers)
			output.Users[userIndex].Tasks[taskIndex].Request.Checks = append([]suites.LoadThreshold{}, task.Request.Checks...)
		}
	}
	output.Stages = append([]suites.LoadStage{}, input.Stages...)
	output.Thresholds = append([]suites.LoadThreshold{}, input.Thresholds...)
	return &output
}

func cloneStepEvaluation(input *suites.StepEvaluation) *suites.StepEvaluation {
	if input == nil {
		return nil
	}

	output := *input
	output.ExpectLogs = append([]string{}, input.ExpectLogs...)
	output.FailOnLogs = append([]string{}, input.FailOnLogs...)
	if input.ExpectExit != nil {
		value := *input.ExpectExit
		output.ExpectExit = &value
	}
	return &output
}
