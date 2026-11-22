// Package pluginschema checks a plugin step's config against the CUE schema the
// plugin registered with. The platform settings API uses it to answer "would
// this config be accepted", and the runner uses it before dispatch, so a typo
// or a wrong type fails the step with the schema's own message instead of
// reaching the plugin.
package pluginschema

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	cueerrors "cuelang.org/go/cue/errors"
)

// A CUE context is not safe for concurrent use, and steps run in parallel.
var (
	mu  sync.Mutex
	ctx = cuecontext.New()
)

// Check reports whether schema is valid CUE.
func Check(schema string) error {
	mu.Lock()
	defer mu.Unlock()
	if v := ctx.CompileString(schema); v.Err() != nil {
		return fmt.Errorf("schema is not valid CUE: %w", v.Err())
	}
	return nil
}

// Validate unifies config with schema. Every field the schema requires must be
// present and concrete, so a missing required setting is an error rather than
// an incomplete value the plugin would receive as nil. Settings the schema does
// not mention are allowed: the step's config also carries the profile's
// environment.
func Validate(schema string, config map[string]any) error {
	mu.Lock()
	defer mu.Unlock()

	compiled := ctx.CompileString(schema)
	if compiled.Err() != nil {
		return fmt.Errorf("schema is not valid CUE: %w", compiled.Err())
	}
	document, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("config cannot be encoded: %w", err)
	}
	value := ctx.CompileBytes(document)
	if value.Err() != nil {
		return fmt.Errorf("config cannot be read: %w", value.Err())
	}
	if err := compiled.Unify(value).Validate(cue.Concrete(true)); err != nil {
		return fmt.Errorf("%s", describe(err))
	}
	return nil
}

// describe turns a CUE error into what an author can act on: one entry per
// problem, naming the field and saying what is wrong with it. CUE's own text
// runs to source positions in a schema the author never sees, and heads a
// disjunction failure with "N errors in empty disjunction", which names nothing.
func describe(err error) string {
	const limit = 4
	seen := map[string]bool{}
	var entries []string
	for _, e := range cueerrors.Errors(err) {
		format, args := e.Msg()
		message := fmt.Sprintf(format, args...)
		if strings.Contains(message, "errors in empty disjunction") {
			continue
		}
		path := strings.Join(e.Path(), ".")

		var entry string
		switch {
		case strings.HasPrefix(message, "incomplete value") && path != "":
			entry = path + " is required"
		case path != "":
			entry = path + ": " + message
		default:
			entry = message
		}
		if !seen[entry] {
			seen[entry] = true
			entries = append(entries, entry)
		}
	}
	if len(entries) == 0 {
		return strings.TrimSpace(err.Error())
	}
	if len(entries) > limit {
		entries = append(entries[:limit], fmt.Sprintf("(%d more)", len(entries)-limit))
	}
	return strings.Join(entries, "; ")
}
