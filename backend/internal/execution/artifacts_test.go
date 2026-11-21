package execution

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/logstream"
	"github.com/babelsuite/babelsuite/internal/runner"
	"github.com/babelsuite/babelsuite/internal/suites"
)

const (
	junitFiveTwoFailed = `<testsuite name="x" tests="5" failures="2"><testcase name="a"/><testcase name="b"/><testcase name="c"/>` +
		`<testcase name="d"><failure/></testcase><testcase name="e"><failure/></testcase></testsuite>`
	junitTwoPassed = `<testsuite name="x" tests="2" failures="0"><testcase name="a"/><testcase name="b"/></testsuite>`
)

func junitNode(exports ...suites.ArtifactExport) topologyNode {
	return topologyNode{ID: "step", Name: "step", Kind: "test", ArtifactExports: exports}
}

func junitExport(path string) suites.ArtifactExport {
	return suites.ArtifactExport{Path: path, Name: "go-tests", On: "always", Format: "junit"}
}

func foundFile(export, content string) map[string]runner.ArtifactResult {
	return map[string]runner.ArtifactResult{
		export: {Export: export, Files: []runner.ArtifactFile{{Path: export, Content: []byte(content)}}},
	}
}

func buildOne(t *testing.T, export suites.ArtifactExport, collected map[string]runner.ArtifactResult) ExecutionArtifact {
	t.Helper()
	artifacts := materializeExecutionArtifacts(junitNode(export), "healthy", collected)
	if len(artifacts) != 1 {
		t.Fatalf("expected one artifact, got %d", len(artifacts))
	}
	return artifacts[0]
}

func TestMissingExportHasNoSummaryInAnyFormat(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"junit", "ctrf", "cobertura", ""} {
		export := suites.ArtifactExport{Path: "reports/none", Name: "none", On: "always", Format: format}
		collected := map[string]runner.ArtifactResult{"reports/none": {Export: "reports/none", Missing: true}}
		artifact := buildOne(t, export, collected)
		if artifact.Status != artifactMissing {
			t.Fatalf("%q: status = %q, want missing", format, artifact.Status)
		}
		if artifact.TestSummary != nil || artifact.CoverageSummary != nil || artifact.Content != "" {
			t.Fatalf("%q: a missing export must carry no summary or content: %+v", format, artifact)
		}
	}
}

func TestExportTheBackendNeverReportedIsMissing(t *testing.T) {
	t.Parallel()
	artifact := buildOne(t, junitExport("reports/junit.xml"), map[string]runner.ArtifactResult{})
	if artifact.Status != artifactMissing || artifact.TestSummary != nil {
		t.Fatalf("expected a missing artifact with no summary, got %+v", artifact)
	}
}

func TestArtifactStatuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		result runner.ArtifactResult
		want   string
	}{
		{"collected", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Content: []byte(junitTwoPassed)}}}, artifactCollected},
		{"empty file", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Content: []byte("  \n")}}}, artifactEmpty},
		{"invalid xml", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Content: []byte("not xml at all")}}}, artifactInvalid},
		{"wrong root", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Content: []byte("<html></html>")}}}, artifactInvalid},
		{"truncated xml", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Content: []byte("<testsuite tests=\"1\"><testcase")}}}, artifactInvalid},
		{"over the size limit", runner.ArtifactResult{Files: []runner.ArtifactFile{{Path: "r.xml", Truncated: true}}}, artifactTruncated},
		{"unreadable", runner.ArtifactResult{Err: errors.New("permission denied")}, artifactUnreadable},
		{"not collected", runner.ArtifactResult{NotCollected: "remote agents do not return reports yet"}, artifactNotCollected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.result.Export = "r.xml"
			artifact := buildOne(t, junitExport("r.xml"), map[string]runner.ArtifactResult{"r.xml": tc.result})
			if artifact.Status != tc.want {
				t.Fatalf("status = %q (%s), want %q", artifact.Status, artifact.Problem, tc.want)
			}
			if tc.want != artifactCollected && artifact.TestSummary != nil {
				t.Fatalf("a %s report must not carry a summary: %+v", tc.want, artifact.TestSummary)
			}
			if tc.want != artifactCollected && artifact.Problem == "" && tc.want != artifactMissing {
				t.Fatalf("a %s report must say why", tc.want)
			}
		})
	}
}

func TestGlobExportAddsUpEveryFile(t *testing.T) {
	t.Parallel()
	export := junitExport("reports/*.xml")
	collected := map[string]runner.ArtifactResult{"reports/*.xml": {
		Export: "reports/*.xml",
		Files: []runner.ArtifactFile{
			{Path: "reports/a.xml", Content: []byte(junitTwoPassed)},
			{Path: "reports/b.xml", Content: []byte(junitFiveTwoFailed)},
			{Path: "reports/c.xml", Content: []byte(junitTwoPassed)},
		},
	}}
	artifact := buildOne(t, export, collected)
	if artifact.Status != artifactCollected || artifact.TestSummary == nil {
		t.Fatalf("expected a collected summary, got %+v", artifact)
	}
	if got := artifact.TestSummary; got.Total != 9 || got.Failures != 2 || got.Passed != 7 {
		t.Fatalf("expected 9 tests with 2 failures, got %+v", got)
	}
	if len(artifact.Files) != 3 {
		t.Fatalf("expected the three matched files to be listed, got %v", artifact.Files)
	}
	if !strings.Contains(artifact.Content, "==> reports/b.xml <==") {
		t.Fatalf("expected each file's content under its own header, got %q", artifact.Content)
	}
}

func TestOneBadFileInAGlobMakesTheExportInvalid(t *testing.T) {
	t.Parallel()
	collected := map[string]runner.ArtifactResult{"reports/*.xml": {
		Export: "reports/*.xml",
		Files: []runner.ArtifactFile{
			{Path: "reports/a.xml", Content: []byte(junitTwoPassed)},
			{Path: "reports/b.xml", Content: []byte("garbage")},
		},
	}}
	artifact := buildOne(t, junitExport("reports/*.xml"), collected)
	if artifact.Status != artifactInvalid || artifact.TestSummary != nil {
		t.Fatalf("expected invalid with no summary, got %+v", artifact)
	}
}

func TestCoverageExportHasASummaryOnlyWhenCollected(t *testing.T) {
	t.Parallel()
	export := suites.ArtifactExport{Path: "coverage.xml", Name: "cov", On: "always", Format: "cobertura"}
	good := `<coverage line-rate="0.8" branch-rate="0.5" lines-covered="8" lines-valid="10" branches-covered="1" branches-valid="2"/>`
	artifact := buildOne(t, export, foundFile("coverage.xml", good))
	if artifact.Status != artifactCollected || artifact.CoverageSummary == nil || artifact.CoverageSummary.LinesCovered != 8 {
		t.Fatalf("expected a coverage summary, got %+v", artifact)
	}
	bad := buildOne(t, export, foundFile("coverage.xml", "<junk/>"))
	if bad.Status != artifactInvalid || bad.CoverageSummary != nil {
		t.Fatalf("expected invalid coverage with no summary, got %+v", bad)
	}
}

func TestCTRFWithoutResultsIsInvalid(t *testing.T) {
	t.Parallel()
	export := suites.ArtifactExport{Path: "ctrf.json", Name: "ctrf", On: "always", Format: "ctrf"}
	artifact := buildOne(t, export, foundFile("ctrf.json", `{}`))
	if artifact.Status != artifactInvalid {
		t.Fatalf("a CTRF document with no results must be invalid, got %+v", artifact)
	}
	good := buildOne(t, export, foundFile("ctrf.json", `{"results":{"summary":{"tests":3,"passed":2,"failed":1}}}`))
	if good.Status != artifactCollected || good.TestSummary == nil || good.TestSummary.Failures != 1 {
		t.Fatalf("expected a CTRF summary, got %+v", good)
	}
}

func TestGateFailsAReportWithFailures(t *testing.T) {
	t.Parallel()
	verdict := gateTestReports(junitNode(junitExport("r.xml")), foundFile("r.xml", junitFiveTwoFailed))
	if len(verdict.Failures) != 1 || !strings.Contains(verdict.Failures[0], "2 of 5 tests failed") {
		t.Fatalf("expected a failure naming 2 of 5 tests, got %+v", verdict)
	}
}

func TestGateFailsAMissingEmptyAndInvalidReport(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		collected map[string]runner.ArtifactResult
		want      string
	}{
		"missing": {map[string]runner.ArtifactResult{"r.xml": {Export: "r.xml", Missing: true}}, "was not produced"},
		"empty":   {foundFile("r.xml", ""), "is empty"},
		"invalid": {foundFile("r.xml", "oops"), "could not be read"},
		"no tests": {
			foundFile("r.xml", `<testsuite name="x" tests="0"/>`),
			"lists no tests",
		},
		"remote": {map[string]runner.ArtifactResult{"r.xml": {Export: "r.xml", NotCollected: "remote agents do not return reports yet"}}, "was not collected"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			verdict := gateTestReports(junitNode(junitExport("r.xml")), tc.collected)
			if len(verdict.Failures) != 1 || !strings.Contains(verdict.Failures[0], tc.want) {
				t.Fatalf("expected a failure containing %q, got %+v", tc.want, verdict)
			}
		})
	}
}

func TestGatePassesAReportWithNoFailures(t *testing.T) {
	t.Parallel()
	verdict := gateTestReports(junitNode(junitExport("r.xml")), foundFile("r.xml", junitTwoPassed))
	if len(verdict.Failures) != 0 || len(verdict.Warnings) != 0 {
		t.Fatalf("expected a clean verdict, got %+v", verdict)
	}
}

func TestNotEnforcedReportIsShownButNeverFails(t *testing.T) {
	t.Parallel()
	export := junitExport("r.xml")
	export.NotEnforced = true

	missing := gateTestReports(junitNode(export), map[string]runner.ArtifactResult{"r.xml": {Export: "r.xml", Missing: true}})
	if len(missing.Failures) != 0 || len(missing.Warnings) != 1 || !strings.Contains(missing.Warnings[0], "not enforced") {
		t.Fatalf("a missing unenforced report must warn, not fail: %+v", missing)
	}
	failing := gateTestReports(junitNode(export), foundFile("r.xml", junitFiveTwoFailed))
	if len(failing.Failures) != 0 {
		t.Fatalf("an unenforced report with failures must not fail the step: %+v", failing)
	}
}

func TestGateIgnoresCoverageAndFailureTriggeredReports(t *testing.T) {
	t.Parallel()
	coverage := suites.ArtifactExport{Path: "c.xml", Name: "cov", On: "always", Format: "cobertura"}
	onFailure := suites.ArtifactExport{Path: "f.xml", Name: "late", On: "failure", Format: "junit"}
	verdict := gateTestReports(junitNode(coverage, onFailure), map[string]runner.ArtifactResult{})
	if len(verdict.Failures) != 0 {
		t.Fatalf("coverage and failure-triggered exports must not gate a healthy step: %+v", verdict)
	}
}

func TestJUnitCountsTestCasesWhenTheyDisagreeWithTheAttributes(t *testing.T) {
	t.Parallel()
	summary, note, err := parseJUnitReport([]byte(`<testsuite name="x" tests="5" failures="0"><testcase name="a"/><testcase name="b"><failure/></testcase></testsuite>`))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 2 || summary.Failures != 1 || summary.Passed != 1 {
		t.Fatalf("expected the test cases to win, got %+v", summary)
	}
	if note == "" {
		t.Fatal("expected a note that the attributes disagree")
	}
}

func TestJUnitUsesAttributesWhenThereAreNoCases(t *testing.T) {
	t.Parallel()
	summary, note, err := parseJUnitReport([]byte(`<testsuite name="x" tests="4" failures="1" skipped="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 4 || summary.Failures != 1 || summary.Skipped != 1 || summary.Passed != 2 || note != "" {
		t.Fatalf("unexpected summary %+v note %q", summary, note)
	}
}

func TestJUnitTestSuitesRootAttributesWithoutChildren(t *testing.T) {
	t.Parallel()
	summary, _, err := parseJUnitReport([]byte(`<testsuites tests="3" failures="1"/>`))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 || summary.Failures != 1 || summary.Passed != 2 {
		t.Fatalf("unexpected summary %+v", summary)
	}
}

func TestJUnitNestedSuitesAreAddedUp(t *testing.T) {
	t.Parallel()
	summary, _, err := parseJUnitReport([]byte(`<testsuites><testsuite name="a"><testsuite name="b"><testcase name="1"/><testcase name="2"><failure/></testcase></testsuite></testsuite><testsuite name="c"><testcase name="3"/></testsuite></testsuites>`))
	if err != nil {
		t.Fatal(err)
	}
	if summary.Total != 3 || summary.Failures != 1 || summary.Passed != 2 {
		t.Fatalf("unexpected summary %+v", summary)
	}
}

// reportBackend reports canned results for a step's exports, as a backend does
// after its container exits.
type reportBackend struct {
	results []runner.ArtifactResult
}

func (b *reportBackend) ID() string                       { return "report" }
func (b *reportBackend) Label() string                    { return "Report" }
func (b *reportBackend) Kind() string                     { return "report" }
func (b *reportBackend) IsAvailable(context.Context) bool { return true }
func (b *reportBackend) Run(_ context.Context, step runner.StepSpec, _ func(logstream.Line)) error {
	for _, result := range b.results {
		step.OnArtifact(result)
	}
	return nil
}

func runReportStep(t *testing.T, export suites.ArtifactExport, results ...runner.ArtifactResult) error {
	t.Helper()
	service := NewService(suites.NewWorkspaceService())
	defer service.Close()

	suite := &suites.Definition{ID: "report-suite", Title: "Report Suite", Topology: []suites.TopologyNode{{ID: "tests"}}}
	node := topologyNode{ID: "tests", Name: "tests", Kind: "test", Variant: "test.run", ArtifactExports: []suites.ArtifactExport{export}}
	return service.runNode(context.Background(), "run-report", suite, "local.yaml", &reportBackend{results: results}, node)
}

func TestStepFailsWhenItsReportListsFailures(t *testing.T) {
	t.Parallel()
	err := runReportStep(t, junitExport("r.xml"), foundFile("r.xml", junitFiveTwoFailed)["r.xml"])
	if err == nil || !strings.Contains(err.Error(), "2 of 5 tests failed") {
		t.Fatalf("expected the step to fail on its report, got %v", err)
	}
}

func TestStepFailsWhenItsReportIsMissing(t *testing.T) {
	t.Parallel()
	err := runReportStep(t, junitExport("r.xml"), runner.ArtifactResult{Export: "r.xml", Missing: true})
	if err == nil || !strings.Contains(err.Error(), "was not produced") {
		t.Fatalf("expected the step to fail on its missing report, got %v", err)
	}
}

func TestStepPassesWithAGoodReport(t *testing.T) {
	t.Parallel()
	if err := runReportStep(t, junitExport("r.xml"), foundFile("r.xml", junitTwoPassed)["r.xml"]); err != nil {
		t.Fatalf("expected the step to pass, got %v", err)
	}
}

func TestStepPassesWhenAMissingReportIsNotEnforced(t *testing.T) {
	t.Parallel()
	export := junitExport("r.xml")
	export.NotEnforced = true
	if err := runReportStep(t, export, runner.ArtifactResult{Export: "r.xml", Missing: true}); err != nil {
		t.Fatalf("expected the step to pass, got %v", err)
	}
}

// runReportSuite runs a one-step suite on the local Docker backend and returns
// the finished record.
func runReportSuite(t *testing.T, exports, script string) ExecutionRecord {
	t.Helper()
	requireDocker(t)
	source := staticSuiteSource{items: map[string]suites.Definition{
		"report-run": {
			ID:    "report-run",
			Title: "Report Run",
			SuiteStar: `load("@babelsuite/runtime", "test")
report = test.run(name="report", image="bash:5.2", file="report.sh", exports=[` + exports + `])
`,
			SourceFiles: []suites.SourceFile{{Path: "tests/report.sh", Content: script}},
			Profiles:    []suites.ProfileOption{{FileName: "local.yaml", Default: true}},
		},
	}}
	service := NewService(source)
	defer service.Close()

	execution, err := service.CreateExecution(context.Background(), CreateRequest{SuiteID: "report-run", Profile: "local.yaml"})
	if err != nil {
		t.Fatalf("create execution: %v", err)
	}
	waitForExecutionTerminal(t, service, execution.ID)
	record, err := service.GetExecution(execution.ID, "")
	if err != nil {
		t.Fatalf("get execution: %v", err)
	}
	return *record
}

func TestRunFailsWhenTheReportItsStepWroteListsFailures(t *testing.T) {
	record := runReportSuite(t,
		`{"path": "reports/junit.xml", "name": "tests", "format": "junit", "on": "always"}`,
		"mkdir -p reports\ncat > reports/junit.xml <<'XML'\n"+junitFiveTwoFailed+"\nXML\necho written\n")
	if record.Status != "Failed" {
		t.Fatalf("a step whose report lists failures must fail the run, got %s", record.Status)
	}
	if len(record.Artifacts) != 1 || record.Artifacts[0].Status != artifactCollected || record.Artifacts[0].TestSummary == nil || record.Artifacts[0].TestSummary.Failures != 2 {
		t.Fatalf("the report that failed the step must stay visible with its counts, got %+v", record.Artifacts)
	}
}

func TestRunWithAnUnenforcedMissingReportFinishesHealthy(t *testing.T) {
	record := runReportSuite(t,
		`{"path": "reports/junit.xml", "name": "tests", "format": "junit", "on": "always", "enforce": False}`,
		"echo no report\n")
	if record.Status != "Healthy" {
		t.Fatalf("an unenforced missing report must not fail or stall the run, got %s", record.Status)
	}
	if len(record.Artifacts) != 1 || record.Artifacts[0].Status != artifactMissing || record.Artifacts[0].TestSummary != nil {
		t.Fatalf("expected a missing artifact with no summary, got %+v", record.Artifacts)
	}
}
