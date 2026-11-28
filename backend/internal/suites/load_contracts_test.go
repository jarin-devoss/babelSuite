package suites

import (
	"reflect"
	"testing"
)

// The modules a suite loads are read from the syntax tree, as the interpreter reads
// them: a load() in a comment is not one, and each module is listed once.
func TestLoadContractsComeFromTheSyntaxTree(t *testing.T) {
	star := `# load("@babelsuite/commented", "x")
load("@babelsuite/runtime", "service")
load("@babelsuite/kafka", "kafka")
load("@babelsuite/runtime", "test")
note = "load(\"@babelsuite/in-a-string\", \"y\")"
`
	want := []string{"@babelsuite/runtime", "@babelsuite/kafka"}
	if got := extractLoadContracts(star); !reflect.DeepEqual(got, want) {
		t.Fatalf("extractLoadContracts = %v, want %v", got, want)
	}
	if got := extractLoadContracts("this is not starlark ("); len(got) != 0 {
		t.Fatalf("a file that does not parse listed %v", got)
	}
}
