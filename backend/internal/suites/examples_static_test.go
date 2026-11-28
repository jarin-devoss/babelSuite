package suites

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// The checks below run on every PR and read the example suites as files. They stop
// the faults the examples used to carry: a test script that asserts nothing, a
// report that says "no failures" whatever happened, a file no suite uses, a step
// keyword nobody reads, and a .proto that does not compile.

const examplesRoot = "../../../examples/oci-suites"

func exampleSuiteDirs(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(examplesRoot)
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if entry.IsDir() {
			dirs = append(dirs, entry.Name())
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no example suites found")
	}
	return dirs
}

func exampleFiles(t *testing.T, suite string) map[string]string {
	t.Helper()
	files := map[string]string{}
	base := filepath.Join(examplesRoot, suite)
	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, _ := filepath.Rel(base, path)
		files[filepath.ToSlash(rel)] = strings.ReplaceAll(string(data), "\r\n", "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// Every example suite's suite.star evaluates, with its default profile's env, by
// the real interpreter. A keyword a step does not read, a helper that does not
// exist or a bad load fails here.
func TestEveryExampleSuiteEvaluates(t *testing.T) {
	for _, suite := range exampleSuiteDirs(t) {
		suite := suite
		t.Run(suite, func(t *testing.T) {
			files := exampleFiles(t, suite)
			star, ok := files["suite.star"]
			if !ok {
				t.Skip("no suite.star")
			}
			if _, err := evalStarlarkTopologyWithEnv(star, docsModuleResolver, defaultProfileEnvFromFiles(files)); err != nil {
				t.Fatalf("suite.star does not evaluate: %v", err)
			}
		})
	}
}

// defaultProfileEnvFromFiles reads the env: of the profile marked default: true.
func defaultProfileEnvFromFiles(files map[string]string) map[string]string {
	env := map[string]string{}
	names := make([]string, 0)
	for name := range files {
		if strings.HasPrefix(name, "profiles/") && strings.HasSuffix(name, ".yaml") {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		text := files[name]
		if !strings.Contains(text, "default: true") {
			continue
		}
		inEnv := false
		for _, line := range strings.Split(text, "\n") {
			switch {
			case strings.HasPrefix(line, "env:"):
				inEnv = true
			case inEnv && strings.HasPrefix(line, "  ") && strings.Contains(line, ":"):
				key, value, _ := strings.Cut(strings.TrimSpace(line), ":")
				env[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
			case inEnv && line != "" && !strings.HasPrefix(line, " "):
				inEnv = false
			}
		}
	}
	return env
}

var literalAssert = regexp.MustCompile(`(?m)^\s*assert\s+f?(?:"[^"]*"|'[^']*')\s*(?:,.*)?$`)

var assertsSomething = regexp.MustCompile(`(?m)\bassert\b|\bcheck\(|\bexpect\(|\braise\b|sys\.exit\(|process\.exit\(|\$fatal|exit 1|\bfinish\(`)

// A script a suite runs as a test or a task is more than a placeholder: a test
// asserts something, and no script is a print and a constant.
func TestExampleScriptsAreNotPlaceholders(t *testing.T) {
	for _, suite := range exampleSuiteDirs(t) {
		for name, text := range exampleFiles(t, suite) {
			if !strings.HasPrefix(name, "tests/") && !strings.HasPrefix(name, "tasks/") {
				continue
			}
			ext := filepath.Ext(name)
			if ext != ".py" && ext != ".ts" && ext != ".js" && ext != ".sh" {
				continue
			}
			if strings.HasPrefix(name, "tests/") && !assertsSomething.MatchString(text) {
				t.Errorf("%s/%s is a test that asserts nothing", suite, name)
			}
			if literalAssert.MatchString(text) { // assert of a string literal is always true
				t.Errorf("%s/%s asserts a string literal, which is always true", suite, name)
			}
		}
	}
}

var hardCodedFailures = regexp.MustCompile(`failures="0"|failures='0'|failures=\\"0\\"`)

// A JUnit report is written from what ran. A literal failures="0" says nothing
// went wrong whatever happened.
func TestExampleReportsAreNotHardCoded(t *testing.T) {
	for _, suite := range exampleSuiteDirs(t) {
		for name, text := range exampleFiles(t, suite) {
			if hardCodedFailures.MatchString(text) {
				t.Errorf("%s/%s writes a report with a literal failures=\"0\"", suite, name)
			}
		}
	}
}

// Every file of an example suite is used: by suite.star (a file=, a plan=, a
// service file), by a mock, by the contract a mock names, by a profile, or by
// another file that is. A file nothing names is dead weight a reader trusts.
func TestExampleFilesAreAllReferenced(t *testing.T) {
	alwaysUsed := map[string]bool{"suite.star": true, "metadata.yaml": true, "README.md": true, "dependencies.yaml": true, "pre-hook.star": true}
	for _, suite := range exampleSuiteDirs(t) {
		files := exampleFiles(t, suite)
		var everything strings.Builder
		for name, text := range files {
			if strings.HasPrefix(name, "profiles/") || name == "README.md" {
				continue
			}
			everything.WriteString(text)
			everything.WriteString("\n")
		}
		corpus := everything.String()
		for name := range files {
			switch {
			case alwaysUsed[name], strings.HasPrefix(name, "profiles/"):
				continue
			case strings.HasPrefix(name, "mock/"):
				// A mock's example and its metadata name each other (sourceArtifact).
				if strings.HasSuffix(name, ".metadata.yaml") || strings.HasSuffix(name, ".cue") {
					continue
				}
			case strings.HasPrefix(name, "api/"):
				// A contract is named by a mock's contractPath.
				if strings.Contains(corpus, filepath.ToSlash(name)) {
					continue
				}
				if suite == "soap-claims-hub" && strings.HasSuffix(name, ".wsdl") {
					continue
				}
			}
			if strings.Contains(corpus, filepath.Base(name)) {
				continue
			}
			t.Errorf("%s/%s is used by nothing in the suite", suite, name)
		}
	}
}

var protoMessage = regexp.MustCompile(`(?m)^\s*(?:message|enum)\s+(\w+)`)
var protoRPC = regexp.MustCompile(`rpc\s+\w+\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)\s*returns\s*\(\s*(?:stream\s+)?([\w.]+)\s*\)`)

// Every .proto names only messages it defines. (The gRPC front end compiles them
// for real at run time; this keeps an undefined message out of the examples.)
func TestExampleProtosDefineTheirMessages(t *testing.T) {
	for _, suite := range exampleSuiteDirs(t) {
		for name, text := range exampleFiles(t, suite) {
			if !strings.HasSuffix(name, ".proto") {
				continue
			}
			defined := map[string]bool{}
			for _, match := range protoMessage.FindAllStringSubmatch(text, -1) {
				defined[match[1]] = true
			}
			for _, match := range protoRPC.FindAllStringSubmatch(text, -1) {
				for _, used := range match[1:] {
					if !defined[strings.TrimPrefix(used, ".")] && !strings.Contains(used, ".") {
						t.Errorf("%s/%s: rpc uses %s, which it does not define", suite, name, used)
					}
				}
			}
		}
	}
}

var imageSetting = regexp.MustCompile(`(?m)\bimage\s*[=:]\s*"([^"\s]+)"`)

// Every image an example or a module names has a tag that is not "latest" (or a
// digest). An image with no tag changes under a suite that was green yesterday.
func TestExampleAndModuleImagesArePinned(t *testing.T) {
	roots := []string{examplesRoot, "../../../examples/oci-modules", "../stdlib/modules"}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			switch filepath.Ext(path) {
			case ".star", ".yaml", ".py":
			default:
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			for _, match := range imageSetting.FindAllStringSubmatch(string(data), -1) {
				image := match[1]
				if strings.Contains(image, "@sha256:") {
					continue
				}
				name := image[strings.LastIndex(image, "/")+1:]
				tag := ""
				if colon := strings.LastIndex(name, ":"); colon >= 0 {
					tag = name[colon+1:]
				}
				if tag == "" || tag == "latest" {
					t.Errorf("%s names the image %q with no pinned tag", path, image)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

// The examples are written by hand, so none says it is generated: a header that
// says "DO NOT EDIT" on a file people edit sends the next reader to the wrong place.
func TestExampleMocksDoNotClaimToBeGenerated(t *testing.T) {
	for _, suite := range exampleSuiteDirs(t) {
		for name, text := range exampleFiles(t, suite) {
			if strings.HasSuffix(name, ".cue") && strings.Contains(text, "DO NOT EDIT") {
				t.Errorf("%s/%s says it is generated, but it is edited by hand", suite, name)
			}
		}
	}
}
