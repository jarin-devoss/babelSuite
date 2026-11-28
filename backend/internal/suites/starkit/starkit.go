// Package starkit is the Starlark glue the evaluators of suite.star and of traffic
// plans share: the interpreter's options and limits, a namespace of builtins
// (service.run, traffic.user, ...) and the read-only env global. It imports
// nothing of the suites packages.
package starkit

import (
	"fmt"
	"sort"

	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// MaxSteps bounds one evaluation, so a loop that never ends is an error.
const MaxSteps uint64 = 1_000_000

// FileOptions are the language features a suite.star or a plan may use.
var FileOptions = &syntax.FileOptions{
	Set:             true,
	GlobalReassign:  true,
	Recursion:       false,
	TopLevelControl: true,
	While:           false,
}

// Builder is a builtin that builds a value from a call.
type Builder func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

// Namespace is a value whose attributes are builtins: service.run, traffic.user.
type Namespace struct {
	Methods map[string]Builder
	// Renamed maps a method that no longer exists to the call that replaced it,
	// so a suite still using it fails saying what to write instead.
	Renamed map[string]string
}

func (ns *Namespace) String() string        { return "babelsuite.Namespace" }
func (ns *Namespace) Type() string          { return "babelsuite.Namespace" }
func (ns *Namespace) Freeze()               {}
func (ns *Namespace) Truth() starlark.Bool  { return starlark.True }
func (ns *Namespace) Hash() (uint32, error) { return 0, fmt.Errorf("namespace is not hashable") }

func (ns *Namespace) Attr(name string) (starlark.Value, error) {
	fn, ok := ns.Methods[name]
	if !ok {
		if replacement, renamed := ns.Renamed[name]; renamed {
			return nil, fmt.Errorf("%s was renamed to %s", name, replacement)
		}
		return nil, nil
	}
	return starlark.NewBuiltin(name, fn), nil
}

func (ns *Namespace) AttrNames() []string {
	names := make([]string, 0, len(ns.Methods))
	for k := range ns.Methods {
		names = append(names, k)
	}
	return names
}

// FrozenEnv is the `env` global: read-only, so a suite cannot reach into the
// environment it was given and change it.
func FrozenEnv(env map[string]string) *starlark.Dict {
	d := starlark.NewDict(len(env))
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		_ = d.SetKey(starlark.String(key), starlark.String(env[key]))
	}
	d.Freeze()
	return d
}
