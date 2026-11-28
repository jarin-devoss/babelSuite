package suites

import (
	"os"
	"strings"
	"testing"

	"github.com/babelsuite/babelsuite/internal/suites/eval"
)

const (
	keywordDoc   = "../../../docs/runtime-library.md"
	keywordBegin = "<!-- generated:step-keywords -->"
	keywordEnd   = "<!-- /generated:step-keywords -->"
)

// The keyword table in the runtime library page is written from the declarations
// the evaluator checks every call against, so the page cannot say a call takes a
// keyword it rejects. UPDATE_DOCS=1 rewrites the table.
func TestRuntimeLibraryKeywordTableIsCurrent(t *testing.T) {
	data, err := os.ReadFile(keywordDoc)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	begin, end := strings.Index(text, keywordBegin), strings.Index(text, keywordEnd)
	if begin < 0 || end < begin {
		t.Fatalf("%s has no %s ... %s block", keywordDoc, keywordBegin, keywordEnd)
	}
	want := keywordBegin + "\n\n" + eval.KeywordTable() + "\n"
	current := text[begin:end]
	if current == want {
		return
	}
	if os.Getenv("UPDATE_DOCS") == "" {
		t.Fatalf("the keyword table in %s is out of date; run with UPDATE_DOCS=1", keywordDoc)
	}
	updated := text[:begin] + want + text[end:]
	if strings.Contains(string(data), "\r\n") {
		updated = strings.ReplaceAll(updated, "\n", "\r\n")
	}
	if err := os.WriteFile(keywordDoc, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}
}
