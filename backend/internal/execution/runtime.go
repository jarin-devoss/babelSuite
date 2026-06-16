package execution

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/queue"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
)

func NewService(source suiteSource, observers ...Observer) *Service {
	return NewServiceWithPlatform(source, nil, observers...)
}

func (s *Service) ConfigureMockResetter(resetter mockResetter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mockResetter = resetter
}

func (s *Service) Close() {
	if s.signals != nil {
		s.signals.shutdown()
	}
	s.cancel()
	s.queue.Close()
}

func (s *Service) ResolveRef(ref string) (*LaunchSuite, error) {
	suite, err := s.suiteSource.Resolve(ref)
	if err != nil {
		return nil, ErrSuiteNotFound
	}
	backends := s.BackendOptions()
	result := LaunchSuite{
		ID:          suite.ID,
		Title:       suite.Title,
		Repository:  suite.Repository,
		Description: suite.Description,
		Provider:    suite.Provider,
		Status:      suite.Status,
		Profiles:    toExecutionProfiles(suite.Profiles),
		Backends:    append([]BackendOption{}, backends...),
	}
	return &result, nil
}

func (s *Service) ListLaunchSuites() []LaunchSuite {
	backends := s.BackendOptions()
	result := make([]LaunchSuite, 0, len(s.suiteSource.List()))
	for _, suite := range s.suiteSource.List() {
		result = append(result, LaunchSuite{
			ID:          suite.ID,
			Title:       suite.Title,
			Repository:  suite.Repository,
			Description: suite.Description,
			Provider:    suite.Provider,
			Status:      suite.Status,
			Profiles:    toExecutionProfiles(suite.Profiles),
			Backends:    append([]BackendOption{}, backends...),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Title < result[j].Title
	})
	return result
}

func (s *Service) ListExecutions(workspaceID string, offset, limit int) ([]ExecutionSummary, int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all := make([]ExecutionSummary, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		item := s.executions[s.order[i]]
		if item == nil {
			continue
		}
		if workspaceID != "" && item.workspaceID != workspaceID {
			continue
		}
		all = append(all, s.summaryLocked(item))
	}

	total := len(all)
	if offset >= total {
		return []ExecutionSummary{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total
}

func (s *Service) GetExecution(executionID, workspaceID string) (*ExecutionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := s.executions[executionID]
	if item == nil {
		return nil, ErrExecutionNotFound
	}
	if workspaceID != "" && item.workspaceID != workspaceID {
		return nil, ErrExecutionNotFound
	}

	record := item.record
	record.Duration = s.durationLocked(item)
	record.Events = append([]ExecutionEvent{}, item.record.Events...)
	record.Artifacts = cloneExecutionArtifacts(item.record.Artifacts)
	record.Suite = cloneExecutionSuite(item.record.Suite)
	return &record, nil
}

func (s *Service) CreateExecution(ctx context.Context, request CreateRequest) (*ExecutionSummary, error) {
	select {
	case s.concurrencySem <- struct{}{}:
	default:
		return nil, ErrBackendUnavailable
	}

	suiteID := strings.TrimSpace(request.SuiteID)
	s.noteLaunch(ctx, suiteID)

	suite, err := s.suiteSource.Get(suiteID)
	if err != nil {
		suite, err = s.suiteSource.Resolve(suiteID)
	}
	if err != nil {
		<-s.concurrencySem
		s.noteRejectedLaunch(ctx, suiteID, "suite_not_found")
		return nil, ErrSuiteNotFound
	}

	profile := strings.TrimSpace(request.Profile)
	if profile == "" {
		profile = defaultProfile(suite.Profiles)
	}
	if profile != "" && !suiteHasProfile(suite.Profiles, profile) {
		<-s.concurrencySem
		s.noteRejectedLaunch(ctx, suite.ID, "profile_not_found")
		return nil, ErrProfileNotFound
	}

	selectedBackend, err := s.resolveBackend(ctx, request.Backend)
	if err != nil {
		<-s.concurrencySem
		s.noteRejectedLaunch(ctx, suite.ID, "backend_unavailable")
		return nil, err
	}

	meta := s.suiteMeta[suite.ID]
	executionID := "run-" + uuid.NewString()[:8]
	startedAt := time.Now().UTC()
	state := &executionState{
		record: ExecutionRecord{
			ID:        executionID,
			Suite:     buildExecutionSuite(*suite),
			Profile:   profile,
			BackendID: selectedBackend.option.ID,
			Backend:   selectedBackend.option.Label,
			Trigger:   strutil.FirstNonEmpty(meta.DefaultTrigger, "Manual"),
			Status:    "Booting",
			StartedAt: startedAt,
			UpdatedAt: startedAt,
			Author:    meta.Author,
			Commit:    buildCommitHash(suite.ID, executionID),
			Branch:    meta.Branch,
			Message:   meta.Message,
			Events:    []ExecutionEvent{},
			Artifacts: []ExecutionArtifact{},
		},
		workspaceID: request.WorkspaceID,
		stepStatus:  make(map[string]string),
	}

	s.mu.Lock()
	s.executions[executionID] = state
	s.order = append(s.order, executionID)
	s.evictOldExecutionsLocked()
	s.mu.Unlock()
	s.logs.Open(executionID)
	s.schedulePersist()

	go s.bootExecution(executionID, suite, profile, selectedBackend)

	s.mu.Lock()
	summary := s.summaryLocked(state)
	s.mu.Unlock()
	go s.syncObservers(executionID)
	return &summary, nil
}

func (s *Service) bootExecution(executionID string, suite *suites.Definition, profile string, selectedBackend backendBinding) {
	defer func() { <-s.concurrencySem }()

	resolved, err := suites.ResolveRuntimeWithModules(*suite, s.suiteSource.List(), s.pluginAwareModuleResolver())
	if err != nil {
		slog.Error("suite topology resolution failed", "suiteID", suite.ID, "error", err)
		s.noteRejectedLaunch(context.Background(), suite.ID, "invalid_topology")
		s.failBootedExecution(executionID, err)
		return
	}
	suite.Topology = resolved.Nodes
	suite.ResolvedDependencies = resolved.Dependencies
	suite.TopologyError = ""

	runtimeOverlay, err := s.resolveExecutionRuntimeOverlay(context.Background(), suite.ID, profile)
	if err != nil {
		s.noteRejectedLaunch(context.Background(), suite.ID, "profile_runtime_error")
		s.failBootedExecution(executionID, err)
		return
	}

	s.mu.Lock()
	state := s.executions[executionID]
	if state != nil {
		state.runtime = runtimeOverlay
		state.total = len(resolved.Nodes)
		state.stepStatus = make(map[string]string, len(resolved.Nodes))
		for _, node := range resolved.Nodes {
			state.stepStatus[node.ID] = "pending"
		}
		state.record.Suite = buildExecutionSuite(*suite)
	}
	s.mu.Unlock()
	if state == nil {
		return
	}

	os.MkdirAll(runner.ExecutionWorkspaceDir(executionID), 0700) //nolint:errcheck
	s.beginRunObservation(context.Background(), state)

	tasks := make([]queue.Task, 0, len(resolved.Nodes))
	taskIDs := make(map[string]string, len(resolved.Nodes))
	for _, node := range resolved.Nodes {
		taskIDs[node.ID] = executionID + ":" + node.ID
	}
	for _, node := range resolved.Nodes {
		onFailureSet := make(map[string]bool, len(node.OnFailure))
		for _, dep := range node.OnFailure {
			onFailureSet[dep] = true
		}
		dependencies := make([]string, 0, len(node.DependsOn))
		softDeps := make([]string, 0, len(node.OnFailure))
		for _, dependency := range node.DependsOn {
			depTaskID := taskIDs[dependency]
			if depTaskID == "" {
				continue
			}
			if onFailureSet[dependency] {
				softDeps = append(softDeps, depTaskID)
			} else {
				dependencies = append(dependencies, depTaskID)
			}
		}
		node := node
		tasks = append(tasks, queue.Task{
			ID:               taskIDs[node.ID],
			Group:            executionID,
			Name:             node.Name,
			Dependencies:     dependencies,
			SoftDependencies: softDeps,
			LeaseTTL:         8 * time.Second,
			Run: func(ctx context.Context) error {
				return s.runNode(ctx, executionID, suite, profile, selectedBackend.backend, node)
			},
			OnCanceled: func() {
				reason := fmt.Sprintf("[%s] Skipped because a required dependency did not complete successfully.", node.Name)
				finished := s.markNodeSkipped(executionID, node.ID, reason)
				if finished {
					s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
				}
			},
		})
	}

	runner.SetupExecutionNetwork(executionID)
	if err := s.ensureSuiteSidecar(suite, profile); err != nil {
		s.failBootedExecution(executionID, fmt.Errorf("APISIX sidecar failed to start: %w", err))
		return
	}

	if err := s.queue.Enqueue(tasks); err != nil {
		s.noteRejectedLaunch(context.Background(), suite.ID, "enqueue_failed")
		s.failBootedExecution(executionID, err)
		s.mu.Lock()
		delete(s.executions, executionID)
		s.order = filterOut(s.order, executionID)
		s.mu.Unlock()
		return
	}

	go s.syncObservers(executionID)
}

// ensureSuiteSidecar starts the APISIX sidecar for the suite if it is not
// already running. Returns an error when the sidecar is required but fails to
// start — callers should abort the execution in that case.
func (s *Service) ensureSuiteSidecar(suite *suites.Definition, profile string) error {
	if suite == nil || !sidecarNeeded(suite) {
		return nil
	}

	settings, err := s.loadPlatformSettings()
	if err != nil {
		return fmt.Errorf("load platform settings: %w", err)
	}
	if settings == nil {
		return nil
	}

	var sidecarImage, configMountPath string
	for _, agent := range settings.Agents {
		if normalizeBackendKind(agent.Type) == "local" {
			sidecarImage = agent.APISIXSidecar.Image
			configMountPath = agent.APISIXSidecar.ConfigMountPath
			break
		}
	}

	plugins := s.loadRegisteredPlugins()
	customPlugins := make([]apisix.CustomPluginConfig, 0, len(plugins))
	for _, p := range plugins {
		if strings.TrimSpace(p.Lua) == "" {
			continue
		}
		customPlugins = append(customPlugins, apisix.CustomPluginConfig{
			Name:    p.Name,
			Trigger: p.Trigger,
			Lua:     p.Lua,
		})
	}

	suiteConfig := suites.ApisixSuiteConfig(*suite)
	suiteConfig.CustomPlugins = customPlugins

	return runner.EnsureSuiteSidecar(suiteConfig, profile, sidecarImage, configMountPath)
}

func (s *Service) failBootedExecution(executionID string, err error) {
	s.mu.Lock()
	if item := s.executions[executionID]; item != nil {
		item.record.Status = "Failed"
		item.record.UpdatedAt = time.Now().UTC()
	}
	s.mu.Unlock()
	s.finishExecutionObservation(executionID, err)
}

func (s *Service) runNode(ctx context.Context, executionID string, suite *suites.Definition, profile string, backend runner.Backend, node topologyNode) error {
	stepCtx, stepSpan, stepStartedAt := s.beginStepObservation(s.stepContext(executionID), executionID, suite, profile, node)

	if reason, skip := s.shouldSkipNode(executionID, suite, node); skip {
		finished := s.markNodeSkipped(executionID, node.ID, reason)
		s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, nil)
		if finished {
			s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
		}
		return nil
	}

	s.appendEvent(executionID, ExecutionEvent{
		ID:        node.ID + "-start",
		Source:    node.ID,
		Timestamp: s.nextTimestamp(executionID),
		Text:      buildStartMessage(node, suite, profile),
		Status:    "running",
		Level:     "info",
	})

	if err := s.resetMockState(stepCtx, executionID, suite, node); err != nil {
		message := fmt.Sprintf("[%s] Mock reset failed: %v", node.Name, err)
		finished := s.markNodeFailed(executionID, node.ID, message, !node.ContinueOnFailure)
		s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, err)
		if finished {
			s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
		}
		if node.ContinueOnFailure {
			s.appendEvent(executionID, ExecutionEvent{
				ID:        node.ID + "-continued",
				Source:    node.ID,
				Timestamp: s.nextTimestamp(executionID),
				Text:      fmt.Sprintf("[%s] continue_on_failure is enabled; downstream nodes may continue.", node.Name),
				Status:    "failed",
				Level:     "warn",
			})
			return nil
		}
		return err
	}

	collectedFiles := make(map[string][]byte)

	err := backend.Run(stepCtx, runner.StepSpec{
		ExecutionID:      executionID,
		SuiteID:          suite.ID,
		SuiteTitle:       suite.Title,
		SuiteRepository:  suite.Repository,
		Profile:          profile,
		RuntimeProfile:   strutil.FirstNonEmpty(node.RuntimeProfile, profile),
		Env:              s.resolveNodeRuntimeEnv(executionID, node),
		Headers:          cloneRuntimeMap(node.RuntimeHeaders),
		Trigger:          s.executionTrigger(executionID),
		BackendID:        s.executionBackendID(executionID),
		BackendLabel:     s.executionBackendLabel(executionID),
		BackendKind:      backend.Kind(),
		SourceSuiteID:    strutil.FirstNonEmpty(node.SourceSuiteID, suite.ID),
		SourceSuiteTitle: strutil.FirstNonEmpty(node.SourceSuiteTitle, suite.Title),
		SourceRepository: strutil.FirstNonEmpty(node.SourceRepository, suite.Repository),
		SourceVersion:    strutil.FirstNonEmpty(node.SourceVersion, suite.Version),
		ResolvedRef:      node.ResolvedRef,
		Digest:           node.Digest,
		DependencyAlias:  node.DependencyAlias,
		StepIndex:        node.Order,
		TotalSteps:       len(suite.Topology),
		HealthySteps:     s.countHealthySteps(executionID),
		LeaseTTL:         8 * time.Second,
		Load:              suitesCloneLoadSpec(node.Load),
		Security:          node.Security,
		Plugin:            rewritePluginConfigForHostAccess(executionID, suite, node.Plugin),
		RegisteredPlugins: s.loadRegisteredPlugins(),
		Evaluation:       cloneNodeEvaluation(node.Evaluation),
		OnFailure:        append([]string{}, node.OnFailure...),
		ArtifactExports:  cloneNodeArtifactExports(node.ArtifactExports),
		OnArtifact: func(path string, content []byte) {
			if len(content) > 0 {
				collectedFiles[path] = content
			}
		},
		GatewayURL:   resolveGatewayURL(executionID, suite, profile),
		GatewayURLs:  resolveGatewayURLs(executionID, suite, profile),
		PublishPorts: pluginHostPortRefs(suite)[node.Name],
		Node: runner.StepNode{
			ID:          node.ID,
			Name:        node.Name,
			Kind:        node.Kind,
			Variant:     node.Variant,
			Image:       node.Image,
			Message:     node.Message,
			File:        node.File,
			Commands:    append([]string{}, node.Commands...),
			FileContent: resolveNodeFileContent(node.File, suite),
			DependsOn:   append([]string{}, node.DependsOn...),
		},
	}, func(line logstream.Line) {
		s.appendRunnerLog(executionID, node.ID, line)
	})
	if err != nil {
		if errors.Is(err, context.Canceled) && s.executionHasFailed(executionID) && !s.nodeBelongsToFailurePath(executionID, suite, node.ID) {
			finished := s.markNodeSkipped(executionID, node.ID, fmt.Sprintf("[%s] Canceled after a fatal failure in another node.", node.Name))
			s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, nil)
			if finished {
				s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
			}
			return nil
		}
		if errors.Is(err, context.Canceled) {
			finished := s.markNodeFailed(executionID, node.ID, fmt.Sprintf("[%s] Execution canceled before the node became healthy.", node.Name), true)
			s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, context.Canceled)
			if finished {
				s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
			}
			return context.Canceled
		}
		message := fmt.Sprintf("[%s] Runner failed: %v", node.Name, err)
		finished := s.markNodeFailed(executionID, node.ID, message, !node.ContinueOnFailure)
		s.registerStepArtifacts(executionID, node, "failed", collectedFiles)
		s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, err)
		if finished {
			s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
		}
		if node.ContinueOnFailure {
			s.appendEvent(executionID, ExecutionEvent{
				ID:        node.ID + "-continued",
				Source:    node.ID,
				Timestamp: s.nextTimestamp(executionID),
				Text:      fmt.Sprintf("[%s] continue_on_failure is enabled; downstream nodes may continue.", node.Name),
				Status:    "failed",
				Level:     "warn",
			})
			return nil
		}
		return err
	}

	finished := s.markNodeHealthy(executionID, node.ID, buildHealthyMessage(node, suite, profile))
	s.registerStepArtifacts(executionID, node, "healthy", collectedFiles)
	s.finishStepObservation(stepCtx, stepSpan, stepStartedAt, executionID, suite, profile, node, nil)
	if finished {
		s.finishExecutionObservation(executionID, s.executionTerminalError(executionID))
	}

	return nil
}

func cloneNodeArtifactExports(input []suites.ArtifactExport) []runner.ArtifactExport {
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

func cloneNodeEvaluation(input *suites.StepEvaluation) *suites.StepEvaluation {
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

func cloneRuntimeMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}

	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func (s *Service) appendEvent(executionID string, event ExecutionEvent) {
	s.mu.Lock()
	item := s.executions[executionID]
	if item == nil {
		s.mu.Unlock()
		return
	}
	s.ensureStepStatusLocked(item)
	if source := strings.TrimSpace(event.Source); source != "" && isKnownStepStatus(event.Status) {
		if _, exists := item.stepStatus[source]; exists {
			item.stepStatus[source] = event.Status
		}
	}

	item.record.Events = append(item.record.Events, event)
	item.record.UpdatedAt = time.Now().UTC()
	streamEvent := StreamEvent{
		ID:              len(item.record.Events),
		ExecutionID:     executionID,
		ExecutionStatus: item.record.Status,
		Duration:        s.durationLocked(item),
		UpdatedAt:       item.record.UpdatedAt,
		Event:           event,
	}
	subscribers := collectSubscribers(s.subs[executionID])
	s.mu.Unlock()

	s.publish(streamEvent, subscribers)
	s.appendLog(executionID, event)
	s.schedulePersist()
	s.syncObservers(executionID)
}

func resolveNodeFileContent(file string, suite *suites.Definition) string {
	if file == "" || suite == nil {
		return ""
	}
	candidates := []string{file, "tasks/" + file, "tests/" + file}
	for _, sf := range suite.SourceFiles {
		for _, candidate := range candidates {
			if sf.Path == candidate {
				return sf.Content
			}
		}
	}
	return ""
}

func (s *Service) countHealthySteps(executionID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	item := s.executions[executionID]
	if item == nil {
		return 0
	}
	count := 0
	for _, status := range item.stepStatus {
		if status == stepStatusHealthy {
			count++
		}
	}
	return count
}

func (s *Service) nextTimestamp(executionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()

	item := s.executions[executionID]
	if item == nil {
		return "00:00"
	}

	elapsed := time.Since(item.record.StartedAt)
	if elapsed < 0 {
		elapsed = 0
	}
	total := int(elapsed.Seconds())
	return fmt.Sprintf("%02d:%02d", total/60, total%60)
}

func (s *Service) syncObservers(executionID string) {
	if len(s.observers) == 0 {
		return
	}

	snapshot, ok := s.snapshotExecution(executionID)
	if !ok {
		return
	}

	for _, observer := range s.observers {
		if observer == nil {
			continue
		}
		observer.SyncExecution(snapshot)
	}
}

func (s *Service) appendLog(executionID string, event ExecutionEvent) {
	s.logs.Append(executionID, logstream.Line{
		Source:    event.Source,
		Timestamp: event.Timestamp,
		Level:     event.Level,
		Text:      event.Text,
	})
}

func (s *Service) appendRunnerLog(executionID, source string, line logstream.Line) {
	timestamp := line.Timestamp
	if strings.TrimSpace(timestamp) == "" {
		timestamp = s.nextTimestamp(executionID)
	}

	s.logs.Append(executionID, logstream.Line{
		Source:    strutil.FirstNonEmpty(line.Source, source),
		Timestamp: timestamp,
		Level:     strutil.FirstNonEmpty(line.Level, "info"),
		Kind:      line.Kind,
		Text:      line.Text,
	})
	s.schedulePersist()
}

// schedulePersist coalesces rapid log-line writes into a single persist call
// fired at most once per 500 ms, preventing per-line MongoDB write amplification.
func (s *Service) schedulePersist() {
	const debounce = 500 * time.Millisecond
	s.persistMu.Lock()
	defer s.persistMu.Unlock()
	if s.persistPending {
		return
	}
	s.persistPending = true
	s.persistTimer = time.AfterFunc(debounce, func() {
		s.persistMu.Lock()
		s.persistPending = false
		s.persistMu.Unlock()
		s.persistExecutionRuntime()
	})
}

func (s *Service) summaryLocked(item *executionState) ExecutionSummary {
	return ExecutionSummary{
		ID:         item.record.ID,
		SuiteID:    item.record.Suite.ID,
		SuiteTitle: item.record.Suite.Title,
		Profile:    item.record.Profile,
		BackendID:  item.record.BackendID,
		Backend:    item.record.Backend,
		Trigger:    item.record.Trigger,
		Status:     item.record.Status,
		Duration:   s.durationLocked(item),
		StartedAt:  item.record.StartedAt,
	}
}

func (s *Service) durationLocked(item *executionState) string {
	end := item.record.UpdatedAt
	if end.Before(item.record.StartedAt) {
		end = item.record.StartedAt
	}
	return formatDuration(end.Sub(item.record.StartedAt))
}

// evictOldExecutionsLocked removes the oldest terminal executions when the
// in-memory store exceeds maxStoredExecutions. Must be called with s.mu held.
func (s *Service) evictOldExecutionsLocked() {
	for len(s.order) > maxStoredExecutions {
		oldest := s.order[0]
		item := s.executions[oldest]
		if item != nil {
			switch item.record.Status {
			case "Healthy", "Failed":
				delete(s.executions, oldest)
				s.order = s.order[1:]
				continue
			}
		}
		break
	}
}

func filterOut(items []string, target string) []string {
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item != target {
			result = append(result, item)
		}
	}
	return result
}

func (s *Service) executionTrigger(executionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.executions[executionID]; item != nil {
		return item.record.Trigger
	}
	return ""
}

func (s *Service) executionBackendID(executionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.executions[executionID]; item != nil {
		return item.record.BackendID
	}
	return ""
}

func (s *Service) executionBackendLabel(executionID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if item := s.executions[executionID]; item != nil {
		return item.record.Backend
	}
	return ""
}

// resolveGatewayURL returns all APISIX sidecar addresses for this execution,
// one per mock node, ordered by topology position. Each mock node runs its own
// APISIX sidecar container whose name follows the Docker runner pattern:
// sidecarNeeded reports whether the suite has any node that requires the
// APISIX sidecar: mock (routing), traffic (traffic-cannon), security
// (attack-scanner), or plugin (user Lua plugins).
func sidecarNeeded(suite *suites.Definition) bool {
	for _, node := range suite.Topology {
		switch node.Kind {
		case suites.NodeKindMock, suites.NodeKindTraffic, suites.NodeKindSecurity, suites.NodeKindPlugin:
			return true
		}
	}
	return false
}

// resolveGatewayURLs returns the APISIX sidecar URL for each mock node in
// topology order, or a single-element slice when the sidecar is needed for
// traffic/security/plugin nodes but there are no mock nodes.
func resolveGatewayURLs(executionID string, suite *suites.Definition, profile string) []string {
	if suite == nil || !sidecarNeeded(suite) {
		return nil
	}

	if url := runner.SuiteSidecarURL(suite.ID, profile); url != "" {
		mockCount := 0
		for _, node := range suite.Topology {
			if node.Kind == suites.NodeKindMock {
				mockCount++
			}
		}
		if mockCount == 0 {
			return []string{url}
		}
		urls := make([]string, mockCount)
		for i := range urls {
			urls[i] = url
		}
		return urls
	}

	// Fallback: container-hostname URLs (works inside Docker networks for K8s / remote agents).
	var urls []string
	for _, node := range suite.Topology {
		if node.Kind != suites.NodeKindMock {
			continue
		}
		host := "babel-" + sanitizeContainerID(executionID) + "-" + sanitizeContainerID(node.ID)
		urls = append(urls, "http://"+host+":9080")
	}
	return urls
}

func resolveGatewayURL(executionID string, suite *suites.Definition, profile string) string {
	urls := resolveGatewayURLs(executionID, suite, profile)
	if len(urls) == 0 {
		return ""
	}
	return urls[0]
}

var pluginHostPortPattern = regexp.MustCompile(`([a-zA-Z0-9_.-]+):(\d{2,5})`)

// pluginHostPortRefs scans every plugin node's config for "<host>:<port>"
// string values where host matches another node's name, and returns the set
// of container ports (as "8082/tcp") each referenced node must publish to the
// host. The shared APISIX sidecar never joins per-execution Docker networks
// (see RemoveExecutionNetwork), so a plugin reaching a sibling service — e.g.
// the consumer-lag plugin's "http://broker:8082" — has to go through a
// host-published port instead of the service's in-network DNS alias.
func pluginHostPortRefs(suite *suites.Definition) map[string][]string {
	if suite == nil {
		return nil
	}
	nodeNames := make(map[string]bool, len(suite.Topology))
	for _, n := range suite.Topology {
		nodeNames[n.Name] = true
	}
	refs := make(map[string][]string)
	for _, n := range suite.Topology {
		if n.Plugin == nil {
			continue
		}
		cfg, ok := n.Plugin.Config.(map[string]any)
		if !ok {
			continue
		}
		for _, v := range cfg {
			s, ok := v.(string)
			if !ok {
				continue
			}
			for _, m := range pluginHostPortPattern.FindAllStringSubmatch(s, -1) {
				host, port := m[1], m[2]
				if !nodeNames[host] {
					continue
				}
				cport := port + "/tcp"
				found := false
				for _, existing := range refs[host] {
					if existing == cport {
						found = true
						break
					}
				}
				if !found {
					refs[host] = append(refs[host], cport)
				}
			}
		}
	}
	return refs
}

// rewritePluginConfigForHostAccess replaces "<host>:<port>" references to
// sibling nodes inside a plugin's config with "host.docker.internal:<port>",
// using the host port that node published via pluginHostPortRefs /
// StepSpec.PublishPorts. Falls back to the original value when that node
// hasn't published a matching port yet (e.g. not running, or no match).
func rewritePluginConfigForHostAccess(executionID string, suite *suites.Definition, spec *suites.PluginSpec) *suites.PluginSpec {
	if spec == nil || suite == nil {
		return spec
	}
	cfg, ok := spec.Config.(map[string]any)
	if !ok {
		return spec
	}
	nodeIDByName := make(map[string]string, len(suite.Topology))
	for _, n := range suite.Topology {
		nodeIDByName[n.Name] = n.ID
	}
	newCfg := make(map[string]any, len(cfg))
	for k, v := range cfg {
		s, ok := v.(string)
		if !ok {
			newCfg[k] = v
			continue
		}
		newCfg[k] = pluginHostPortPattern.ReplaceAllStringFunc(s, func(match string) string {
			parts := pluginHostPortPattern.FindStringSubmatch(match)
			host, port := parts[1], parts[2]
			nodeID, found := nodeIDByName[host]
			if !found {
				return match
			}
			hostPort := runner.ContainerHostPort(executionID, nodeID, port+"/tcp")
			if hostPort == "" {
				return match
			}
			return "host.docker.internal:" + hostPort
		})
	}
	clone := *spec
	clone.Config = newCfg
	return &clone
}

// sanitizeContainerID mirrors the runner's sanitizeID so container names match.
func sanitizeContainerID(id string) string {
	var b strings.Builder
	for _, ch := range id {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9', ch == '-':
			b.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch + 32)
		default:
			b.WriteRune('-')
		}
	}
	s := b.String()
	if len(s) > 40 {
		s = s[:40]
	}
	return strings.Trim(s, "-")
}
