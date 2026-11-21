package execution

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

type junitSuiteDocument struct {
	XMLName  xml.Name             `xml:"testsuite"`
	Name     string               `xml:"name,attr"`
	Tests    string               `xml:"tests,attr"`
	Failures string               `xml:"failures,attr"`
	Errors   string               `xml:"errors,attr"`
	Skipped  string               `xml:"skipped,attr"`
	Time     string               `xml:"time,attr"`
	Suites   []junitSuiteDocument `xml:"testsuite"`
	Cases    []junitCaseDocument  `xml:"testcase"`
}

type junitSuitesDocument struct {
	XMLName  xml.Name             `xml:"testsuites"`
	Tests    string               `xml:"tests,attr"`
	Failures string               `xml:"failures,attr"`
	Errors   string               `xml:"errors,attr"`
	Skipped  string               `xml:"skipped,attr"`
	Suites   []junitSuiteDocument `xml:"testsuite"`
}

type junitCaseDocument struct {
	Time    string                `xml:"time,attr"`
	Failure *junitFailureDocument `xml:"failure"`
	Error   *junitFailureDocument `xml:"error"`
	Skipped *junitSkippedDocument `xml:"skipped"`
}

type junitFailureDocument struct{}

type junitSkippedDocument struct{}

type coberturaDocument struct {
	XMLName         xml.Name `xml:"coverage"`
	LineRate        string   `xml:"line-rate,attr"`
	BranchRate      string   `xml:"branch-rate,attr"`
	LinesCovered    string   `xml:"lines-covered,attr"`
	LinesValid      string   `xml:"lines-valid,attr"`
	BranchesCovered string   `xml:"branches-covered,attr"`
	BranchesValid   string   `xml:"branches-valid,attr"`
}

func normalizeArtifactFormat(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func parseJUnitSummary(document []byte) (*ExecutionTestSummary, error) {
	summary, _, err := parseJUnitReport(document)
	return summary, err
}

// parseJUnitReport reads a JUnit document. The test cases are counted when the
// file lists them, because they are the evidence; the counts in the suite's own
// attributes are used only for a file that lists no cases. When the two
// disagree, the note says so.
func parseJUnitReport(document []byte) (*ExecutionTestSummary, string, error) {
	trimmed := bytes.TrimSpace(document)
	if len(trimmed) == 0 {
		return nil, "", fmt.Errorf("empty junit document")
	}

	var probe struct {
		XMLName xml.Name
	}
	if err := xml.Unmarshal(trimmed, &probe); err != nil {
		return nil, "", err
	}

	var notes []string
	switch probe.XMLName.Local {
	case "testsuite":
		var suite junitSuiteDocument
		if err := xml.Unmarshal(trimmed, &suite); err != nil {
			return nil, "", err
		}
		summary := summarizeJUnitSuite(suite, &notes)
		return &summary, strings.Join(notes, "; "), nil
	case "testsuites":
		var suites junitSuitesDocument
		if err := xml.Unmarshal(trimmed, &suites); err != nil {
			return nil, "", err
		}
		summary := ExecutionTestSummary{}
		for _, suite := range suites.Suites {
			child := summarizeJUnitSuite(suite, &notes)
			summary.Total += child.Total
			summary.Passed += child.Passed
			summary.Failures += child.Failures
			summary.Errors += child.Errors
			summary.Skipped += child.Skipped
			summary.DurationSeconds += child.DurationSeconds
		}
		if len(suites.Suites) == 0 && (suites.Tests != "" || suites.Failures != "") {
			summary = ExecutionTestSummary{
				Total:    parseXMLInt(suites.Tests),
				Failures: parseXMLInt(suites.Failures),
				Errors:   parseXMLInt(suites.Errors),
				Skipped:  parseXMLInt(suites.Skipped),
			}
			summary.Passed = max(0, summary.Total-summary.Failures-summary.Errors-summary.Skipped)
		}
		return &summary, strings.Join(notes, "; "), nil
	default:
		return nil, "", fmt.Errorf("unsupported junit root element %q", probe.XMLName.Local)
	}
}

func summarizeJUnitSuite(suite junitSuiteDocument, notes *[]string) ExecutionTestSummary {
	if len(suite.Suites) > 0 {
		summary := ExecutionTestSummary{}
		for _, childSuite := range suite.Suites {
			child := summarizeJUnitSuite(childSuite, notes)
			summary.Total += child.Total
			summary.Passed += child.Passed
			summary.Failures += child.Failures
			summary.Errors += child.Errors
			summary.Skipped += child.Skipped
			summary.DurationSeconds += child.DurationSeconds
		}
		return summary
	}

	declared := ExecutionTestSummary{
		Total:           parseXMLInt(suite.Tests),
		Failures:        parseXMLInt(suite.Failures),
		Errors:          parseXMLInt(suite.Errors),
		Skipped:         parseXMLInt(suite.Skipped),
		DurationSeconds: parseXMLFloat(suite.Time),
	}
	if len(suite.Cases) == 0 {
		declared.Passed = max(0, declared.Total-declared.Failures-declared.Errors-declared.Skipped)
		return declared
	}

	counted := ExecutionTestSummary{Total: len(suite.Cases)}
	var caseSeconds float64
	for _, testCase := range suite.Cases {
		switch {
		case testCase.Failure != nil:
			counted.Failures++
		case testCase.Error != nil:
			counted.Errors++
		case testCase.Skipped != nil:
			counted.Skipped++
		}
		caseSeconds += parseXMLFloat(testCase.Time)
	}
	counted.Passed = counted.Total - counted.Failures - counted.Errors - counted.Skipped
	counted.DurationSeconds = declared.DurationSeconds
	if counted.DurationSeconds == 0 {
		counted.DurationSeconds = caseSeconds
	}

	disagrees := (declared.Total != 0 && declared.Total != counted.Total) ||
		declared.Failures != counted.Failures ||
		declared.Errors != counted.Errors
	if disagrees && notes != nil {
		*notes = append(*notes, fmt.Sprintf("the suite says %d tests, %d failures and %d errors, but its %d test cases show %d failures and %d errors; counted the test cases",
			declared.Total, declared.Failures, declared.Errors, counted.Total, counted.Failures, counted.Errors))
	}
	return counted
}

func parseCoberturaSummary(document []byte) (*ExecutionCoverageSummary, error) {
	trimmed := bytes.TrimSpace(document)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty cobertura document")
	}

	var coverage coberturaDocument
	if err := xml.Unmarshal(trimmed, &coverage); err != nil {
		return nil, err
	}
	if coverage.XMLName.Local != "coverage" {
		return nil, fmt.Errorf("unsupported cobertura root element %q", coverage.XMLName.Local)
	}

	return &ExecutionCoverageSummary{
		LineRate:        parseXMLFloat(coverage.LineRate),
		BranchRate:      parseXMLFloat(coverage.BranchRate),
		LinesCovered:    parseXMLInt(coverage.LinesCovered),
		LinesValid:      parseXMLInt(coverage.LinesValid),
		BranchesCovered: parseXMLInt(coverage.BranchesCovered),
		BranchesValid:   parseXMLInt(coverage.BranchesValid),
	}, nil
}

type ctrfDocument struct {
	Results *struct {
		Summary struct {
			Tests   int   `json:"tests"`
			Passed  int   `json:"passed"`
			Failed  int   `json:"failed"`
			Skipped int   `json:"skipped"`
			Pending int   `json:"pending"`
			Other   int   `json:"other"`
			Start   int64 `json:"start"`
			Stop    int64 `json:"stop"`
		} `json:"summary"`
		Tests []struct {
			Status   string  `json:"status"`
			Duration float64 `json:"duration"`
		} `json:"tests"`
	} `json:"results"`
}

func parseCTRFSummary(document []byte) (*ExecutionTestSummary, error) {
	trimmed := bytes.TrimSpace(document)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty ctrf document")
	}

	var doc ctrfDocument
	if err := json.Unmarshal(trimmed, &doc); err != nil {
		return nil, err
	}

	if doc.Results == nil {
		return nil, fmt.Errorf("ctrf document has no results object")
	}
	s := doc.Results.Summary
	total := s.Tests
	if total == 0 {
		total = len(doc.Results.Tests)
	}

	var durationMs float64
	if s.Start > 0 && s.Stop > s.Start {
		durationMs = float64(s.Stop - s.Start)
	} else {
		for _, t := range doc.Results.Tests {
			durationMs += t.Duration
		}
	}

	errors := s.Other
	passed := s.Passed
	failures := s.Failed
	skipped := s.Skipped + s.Pending

	if total > 0 && passed == 0 && failures == 0 && skipped == 0 {
		for _, t := range doc.Results.Tests {
			switch strings.ToLower(t.Status) {
			case "passed":
				passed++
			case "failed":
				failures++
			case "skipped", "pending":
				skipped++
			default:
				errors++
			}
		}
	}

	return &ExecutionTestSummary{
		Total:           total,
		Passed:          passed,
		Failures:        failures,
		Errors:          errors,
		Skipped:         skipped,
		DurationSeconds: durationMs / 1000,
	}, nil
}

func parseXMLInt(value string) int {
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0
	}
	return parsed
}

func parseXMLFloat(value string) float64 {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0
	}
	return parsed
}
