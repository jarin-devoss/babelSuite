package execution

import (
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// What became of a declared export. Only a collected one carries a summary.
const (
	artifactCollected    = "collected"
	artifactMissing      = "missing"
	artifactEmpty        = "empty"
	artifactInvalid      = "invalid"
	artifactTruncated    = "truncated"
	artifactUnreadable   = "unreadable"
	artifactNotCollected = "not_collected"
)

// registerStepArtifacts records the step's exports on the execution. When
// includeSuccess is set, exports declared for success are kept although the
// step failed: a report that failed the step has to stay visible.
func (s *Service) registerStepArtifacts(executionID string, node topologyNode, status string, includeSuccess bool, collected map[string]runner.ArtifactResult) {
	artifacts := materializeExecutionArtifacts(node, status, collected)
	if includeSuccess && status != "healthy" {
		artifacts = append(artifacts, materializeExecutionArtifacts(node, "healthy", collected)...)
	}
	if len(artifacts) == 0 {
		return
	}

	s.mu.Lock()
	item := s.executions[executionID]
	if item == nil {
		s.mu.Unlock()
		return
	}

	added := false
	for _, artifact := range artifacts {
		if executionArtifactExists(item.record.Artifacts, artifact.ID) {
			continue
		}
		item.record.Artifacts = append(item.record.Artifacts, artifact)
		added = true
	}
	if added {
		item.record.UpdatedAt = time.Now().UTC()
	}
	s.mu.Unlock()

	if added {
		s.schedulePersist()
		s.syncObservers(executionID)
	}
}

func materializeExecutionArtifacts(node topologyNode, status string, collected map[string]runner.ArtifactResult) []ExecutionArtifact {
	if len(node.ArtifactExports) == 0 {
		return nil
	}

	result := make([]ExecutionArtifact, 0, len(node.ArtifactExports))
	for _, export := range node.ArtifactExports {
		if !artifactExportMatchesStatus(export.On, status) {
			continue
		}
		result = append(result, buildExecutionArtifact(node, export, collected))
	}
	return result
}

// buildExecutionArtifact turns what a backend found for an export into the
// artifact shown to the user. A summary exists only when a real, readable
// report was read: a report nobody produced is never filled in.
func buildExecutionArtifact(node topologyNode, export suites.ArtifactExport, collected map[string]runner.ArtifactResult) ExecutionArtifact {
	format := normalizeArtifactFormat(export.Format)
	artifact := ExecutionArtifact{
		ID:       executionArtifactID(node.ID, export),
		StepID:   node.ID,
		StepName: node.Name,
		Path:     strings.TrimSpace(export.Path),
		Name:     executionArtifactName(export),
		On:       strutil.FirstNonEmpty(strings.TrimSpace(export.On), "success"),
		Format:   format,
	}

	result, found := collected[strings.TrimSpace(export.Path)]
	switch {
	case !found:
		artifact.Status = artifactMissing
		artifact.Problem = "the backend did not report this export"
		return artifact
	case result.NotCollected != "":
		artifact.Status = artifactNotCollected
		artifact.Problem = result.NotCollected
		return artifact
	case result.Err != nil:
		artifact.Status = artifactUnreadable
		artifact.Problem = result.Err.Error()
		return artifact
	case result.Missing || len(result.Files) == 0:
		artifact.Status = artifactMissing
		return artifact
	}

	for _, file := range result.Files {
		artifact.Files = append(artifact.Files, file.Path)
	}
	if len(result.Files) == 1 && !strings.ContainsAny(artifact.Path, "*?[") {
		artifact.Files = nil
	}

	var oversize, blank []string
	contents := make([][]byte, 0, len(result.Files))
	for _, file := range result.Files {
		switch {
		case file.Truncated:
			oversize = append(oversize, file.Path)
		case len(strings.TrimSpace(string(file.Content))) == 0:
			blank = append(blank, file.Path)
		default:
			contents = append(contents, file.Content)
		}
	}
	artifact.Content = joinArtifactContent(result.Files)

	switch {
	case len(oversize) > 0:
		artifact.Status = artifactTruncated
		artifact.Problem = fmt.Sprintf("%s is over the size limit and was not read", strings.Join(oversize, ", "))
		return artifact
	case len(contents) == 0:
		artifact.Status = artifactEmpty
		artifact.Problem = fmt.Sprintf("%s is empty", strings.Join(blank, ", "))
		return artifact
	case len(blank) > 0:
		artifact.Status = artifactInvalid
		artifact.Problem = fmt.Sprintf("%s is empty", strings.Join(blank, ", "))
		return artifact
	}

	artifact.Status = artifactCollected
	switch format {
	case "junit", "ctrf":
		summary, note, err := summarizeTestReports(format, contents)
		if err != nil {
			artifact.Status = artifactInvalid
			artifact.Problem = err.Error()
			return artifact
		}
		artifact.TestSummary = summary
		artifact.Problem = note
	case "cobertura":
		summary, err := parseCoberturaSummary(contents[0])
		if err != nil {
			artifact.Status = artifactInvalid
			artifact.Problem = err.Error()
			return artifact
		}
		artifact.CoverageSummary = summary
		if len(contents) > 1 {
			artifact.Problem = "only the first coverage file is summarised"
		}
	}
	return artifact
}

// summarizeTestReports adds up the summaries of every report file of an export.
func summarizeTestReports(format string, documents [][]byte) (*ExecutionTestSummary, string, error) {
	total := ExecutionTestSummary{}
	var notes []string
	for _, document := range documents {
		var (
			summary *ExecutionTestSummary
			note    string
			err     error
		)
		if format == "ctrf" {
			summary, err = parseCTRFSummary(document)
		} else {
			summary, note, err = parseJUnitReport(document)
		}
		if err != nil {
			return nil, "", err
		}
		if note != "" {
			notes = append(notes, note)
		}
		total.Total += summary.Total
		total.Passed += summary.Passed
		total.Failures += summary.Failures
		total.Errors += summary.Errors
		total.Skipped += summary.Skipped
		total.DurationSeconds += summary.DurationSeconds
	}
	return &total, strings.Join(notes, "; "), nil
}

// joinArtifactContent is the text kept for the user to read. Several files are
// separated by a header naming each.
func joinArtifactContent(files []runner.ArtifactFile) string {
	if len(files) == 1 {
		return string(files[0].Content)
	}
	var builder strings.Builder
	for _, file := range files {
		if len(file.Content) == 0 {
			continue
		}
		fmt.Fprintf(&builder, "==> %s <==\n%s\n", file.Path, strings.TrimRight(string(file.Content), "\n"))
	}
	return builder.String()
}

func executionArtifactExists(items []ExecutionArtifact, targetID string) bool {
	for _, item := range items {
		if item.ID == targetID {
			return true
		}
	}
	return false
}

func artifactExportMatchesStatus(trigger, status string) bool {
	switch strings.TrimSpace(trigger) {
	case "", "success":
		return status == "healthy"
	case "failure":
		return status == "failed"
	case "always":
		return status == "healthy" || status == "failed"
	default:
		return false
	}
}

func executionArtifactName(export suites.ArtifactExport) string {
	if name := strings.TrimSpace(export.Name); name != "" {
		return name
	}
	base := strings.TrimSpace(path.Base(strings.TrimSpace(export.Path)))
	if base == "." || base == "/" || base == "" {
		return strings.TrimSpace(export.Path)
	}
	return base
}

func executionArtifactID(stepID string, export suites.ArtifactExport) string {
	replacer := strings.NewReplacer("/", "-", "\\", "-", " ", "-", "*", "wildcard", ":", "-")
	format := normalizeArtifactFormat(export.Format)
	if format == "" {
		format = "raw"
	}
	return replacer.Replace(stepID + "-" + executionArtifactName(export) + "-" + format)
}

// reportVerdict is what the report gate found wrong with a step's reports.
type reportVerdict struct {
	// Failures are the reports that fail the step, one sentence each.
	Failures []string
	// Warnings are reports that are not collected but are not enforced.
	Warnings []string
}

// gateTestReports checks the structured reports a step declared for success.
// A junit or ctrf export is a claim about tests, so the step fails when the
// report was not produced or cannot be read, lists no tests, or lists failures.
// An export declared with enforce=False is shown but never fails the step.
func gateTestReports(node topologyNode, collected map[string]runner.ArtifactResult) reportVerdict {
	var verdict reportVerdict
	for _, export := range node.ArtifactExports {
		format := normalizeArtifactFormat(export.Format)
		if format != "junit" && format != "ctrf" {
			continue
		}
		if !artifactExportMatchesStatus(export.On, "healthy") {
			continue
		}

		artifact := buildExecutionArtifact(node, export, collected)
		label := fmt.Sprintf("report %q (%s)", artifact.Name, artifact.Path)

		var failure string
		switch {
		case artifact.Status != artifactCollected:
			failure = fmt.Sprintf("%s %s", label, describeArtifactStatus(artifact))
		case artifact.TestSummary == nil || artifact.TestSummary.Total == 0:
			failure = fmt.Sprintf("%s lists no tests", label)
		case artifact.TestSummary.Failures+artifact.TestSummary.Errors > 0:
			bad := artifact.TestSummary.Failures + artifact.TestSummary.Errors
			failure = fmt.Sprintf("%s: %d of %d tests failed", label, bad, artifact.TestSummary.Total)
		}
		if failure == "" {
			continue
		}
		if export.NotEnforced {
			if artifact.Status != artifactCollected {
				verdict.Warnings = append(verdict.Warnings, failure+"; not enforced")
			}
			continue
		}
		verdict.Failures = append(verdict.Failures, failure)
	}
	return verdict
}

func describeArtifactStatus(artifact ExecutionArtifact) string {
	var what string
	switch artifact.Status {
	case artifactMissing:
		what = "was not produced"
	case artifactEmpty:
		what = "is empty"
	case artifactInvalid:
		what = "could not be read"
	case artifactTruncated:
		what = "is too large to read"
	case artifactUnreadable:
		what = "could not be read"
	case artifactNotCollected:
		what = "was not collected"
	default:
		what = "has status " + artifact.Status
	}
	if artifact.Problem != "" {
		return what + ": " + artifact.Problem
	}
	return what
}

// reportGateError is the failure of a step whose own checks passed but whose
// test reports did not.
type reportGateError struct {
	failures []string
}

func (e *reportGateError) Error() string {
	return strings.Join(e.failures, "; ")
}
