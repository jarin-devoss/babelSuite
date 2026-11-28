package eval

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/babelsuite/babelsuite/internal/stdlib"
	"github.com/babelsuite/babelsuite/internal/suites/model"
	"github.com/babelsuite/babelsuite/internal/suites/starkit"
	"go.starlark.net/starlark"
)

// starlarkNode is a step a suite.star call created: the fields every step has,
// and the spec of its kind (stepkind.go).
type starlarkNode struct {
	stepCommon
	spec stepSpec
}

func (n *starlarkNode) String() string        { return n.name }
func (n *starlarkNode) Type() string          { return "babelsuite.Node" }
func (n *starlarkNode) Freeze()               {}
func (n *starlarkNode) Truth() starlark.Bool  { return starlark.True }
func (n *starlarkNode) Hash() (uint32, error) { return 0, fmt.Errorf("node is not hashable") }
func (n *starlarkNode) AttrNames() []string   { return []string{"env", "name"} }
func (n *starlarkNode) Attr(attr string) (starlark.Value, error) {
	switch attr {
	case "name":
		return starlark.String(n.name), nil
	case "env":
		// What the node was created with, read-only, so a helper reads the port and
		// credentials a service was given instead of guessing its defaults.
		var env map[string]string
		if r, ok := n.spec.(runner); ok {
			env = r.runPart().env
		}
		return starkit.FrozenEnv(env), nil
	}
	return nil, nil
}

type starlarkRegistry struct {
	mu    sync.Mutex
	nodes []*starlarkNode
}

func (r *starlarkRegistry) register(n *starlarkNode) {
	r.mu.Lock()
	n.order = len(r.nodes)
	r.nodes = append(r.nodes, n)
	r.mu.Unlock()
}

func Eval(suiteStar string, resolve ModuleResolver) ([]RawNode, error) {
	return EvalWithEnv(suiteStar, resolve, nil)
}

// EvalWithEnv evaluates suite.star with env as the `env` global:
// the environment the run's profile gives, so `env.get("COUNT", "1")` reads the
// profile instead of always answering its default.
func EvalWithEnv(suiteStar string, resolve ModuleResolver, env map[string]string) (nodes []RawNode, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("starlark evaluation panicked: %v", r)
		}
	}()

	reg := &starlarkRegistry{}

	predeclared, err := buildRuntimePredeclared(reg, env)
	if err != nil {
		return nil, err
	}

	thread := &starlark.Thread{
		Name: "suite.star",
		Load: func(t *starlark.Thread, module string) (starlark.StringDict, error) {
			return resolveStarlarkModule(module, reg, resolve)
		},
	}
	thread.SetMaxExecutionSteps(starkit.MaxSteps)

	globals, err := starlark.ExecFileOptions(starkit.FileOptions, thread, "suite.star", suiteStar, predeclared)
	if err != nil {
		return nil, fmt.Errorf("starlark: %w", err)
	}

	if len(reg.nodes) == 0 {
		return nil, fmt.Errorf("starlark: no topology nodes registered")
	}

	assignIDs(reg, globals)
	return buildRawNodes(reg), nil
}

func buildRuntimePredeclared(reg *starlarkRegistry, env map[string]string) (starlark.StringDict, error) {
	runtimeModule, err := buildRuntimeModule(reg)
	if err != nil {
		return nil, err
	}
	return starlark.StringDict{
		"service":  runtimeModule["service"],
		"mock":     runtimeModule["mock"],
		"task":     runtimeModule["task"],
		"test":     runtimeModule["test"],
		"traffic":  runtimeModule["traffic"],
		"suite":    runtimeModule["suite"],
		"security": runtimeModule["security"],
		"log":      runtimeModule["log"],
		"plugin":   runtimeModule["plugin"],
		"env":      starkit.FrozenEnv(env),
		"utils":    buildUtilsModule(),
	}, nil
}

func buildUtilsModule() *starlarkUtils {
	return &starlarkUtils{members: starlark.StringDict{
		"sanitize":      starlark.NewBuiltin("sanitize", utilsSanitize),
		"merge":         starlark.NewBuiltin("merge", utilsMerge),
		"quoted":        starlark.NewBuiltin("quoted", utilsQuoted),
		"unique":        starlark.NewBuiltin("unique", utilsUnique),
		"sql_value":     starlark.NewBuiltin("sql_value", utilsSqlValue),
		"sql_predicate": starlark.NewBuiltin("sql_predicate", utilsSqlPredicate),
		"js_value":      starlark.NewBuiltin("js_value", utilsJsValue),
	}}
}

type starlarkUtils struct{ members starlark.StringDict }

func (u *starlarkUtils) String() string        { return "utils" }
func (u *starlarkUtils) Type() string          { return "babelsuite.Utils" }
func (u *starlarkUtils) Freeze()               {}
func (u *starlarkUtils) Truth() starlark.Bool  { return starlark.True }
func (u *starlarkUtils) Hash() (uint32, error) { return 0, fmt.Errorf("utils is not hashable") }
func (u *starlarkUtils) Attr(name string) (starlark.Value, error) {
	return u.members[name], nil
}
func (u *starlarkUtils) AttrNames() []string {
	names := make([]string, 0, len(u.members))
	for k := range u.members {
		names = append(names, k)
	}
	return names
}

func utilsSanitize(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("utils.sanitize", args, kwargs, "value", &val); err != nil {
		return nil, err
	}
	s, _ := starlark.AsString(val)
	if s == "" {
		s = val.String()
	}
	var b strings.Builder
	for _, ch := range s {
		switch {
		case ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9':
			b.WriteRune(ch)
		case ch >= 'A' && ch <= 'Z':
			b.WriteRune(ch + 32)
		default:
			b.WriteRune('-')
		}
	}
	result := b.String()
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	result = strings.Trim(result, "-")
	if result == "" {
		result = "step"
	}
	return starlark.String(result), nil
}

func utilsMerge(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var base, overrides *starlark.Dict
	if err := starlark.UnpackArgs("utils.merge", args, kwargs, "base", &base, "overrides", &overrides); err != nil {
		return nil, err
	}
	merged := starlark.NewDict(base.Len() + overrides.Len())
	for _, kv := range base.Items() {
		merged.SetKey(kv[0], kv[1]) //nolint:errcheck
	}
	for _, kv := range overrides.Items() {
		merged.SetKey(kv[0], kv[1]) //nolint:errcheck
	}
	return merged, nil
}

func utilsQuoted(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("utils.quoted", args, kwargs, "value", &val); err != nil {
		return nil, err
	}
	s, _ := starlark.AsString(val)
	if s == "" {
		s = val.String()
	}
	return starlark.String("'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"), nil
}

func utilsUnique(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var list *starlark.List
	var item starlark.Value
	if err := starlark.UnpackArgs("utils.unique", args, kwargs, "values", &list, "item", &item); err != nil {
		return nil, err
	}
	result := starlark.NewList(nil)
	found := false
	for i := 0; i < list.Len(); i++ {
		v := list.Index(i)
		result.Append(v) //nolint:errcheck
		if v.String() == item.String() {
			found = true
		}
	}
	if !found {
		result.Append(item) //nolint:errcheck
	}
	return result, nil
}

func utilsSqlValue(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("utils.sql_value", args, kwargs, "value", &val); err != nil {
		return nil, err
	}
	switch v := val.(type) {
	case starlark.NoneType:
		return starlark.String("null"), nil
	case starlark.Bool:
		if v {
			return starlark.String("true"), nil
		}
		return starlark.String("false"), nil
	case starlark.Int:
		return starlark.String(v.String()), nil
	case starlark.Float:
		return starlark.String(v.String()), nil
	default:
		s, _ := starlark.AsString(val)
		return starlark.String("'" + strings.ReplaceAll(s, "'", "''") + "'"), nil
	}
}

func utilsSqlPredicate(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("utils.sql_predicate", args, kwargs, "where", &val); err != nil {
		return nil, err
	}
	if s, ok := starlark.AsString(val); ok {
		return starlark.String(s), nil
	}
	d, ok := val.(*starlark.Dict)
	if !ok {
		return nil, fmt.Errorf("utils.sql_predicate: where must be a string or dict")
	}
	parts := make([]string, 0, d.Len())
	for _, kv := range d.Items() {
		sqlVal, err := utilsSqlValue(thread, fn, starlark.Tuple{kv[1]}, nil)
		if err != nil {
			return nil, err
		}
		k, _ := starlark.AsString(kv[0])
		parts = append(parts, k+" = "+sqlVal.(starlark.String).GoString())
	}
	return starlark.String(strings.Join(parts, " and ")), nil
}

func utilsJsValue(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var val starlark.Value
	if err := starlark.UnpackArgs("utils.js_value", args, kwargs, "value", &val); err != nil {
		return nil, err
	}
	switch v := val.(type) {
	case starlark.String:
		s, _ := starlark.AsString(val)
		return starlark.String(`"` + strings.ReplaceAll(s, `"`, `\"`) + `"`), nil
	case starlark.Bool:
		if v {
			return starlark.String("true"), nil
		}
		return starlark.String("false"), nil
	case starlark.NoneType:
		return starlark.String("null"), nil
	default:
		return starlark.String(val.String()), nil
	}
}

func resolveStarlarkModule(module string, reg *starlarkRegistry, resolve ModuleResolver) (starlark.StringDict, error) {
	if module == "@babelsuite/runtime" {
		return buildRuntimeModule(reg)
	}
	if strings.HasPrefix(module, "@babelsuite/") {
		suffix := strings.TrimPrefix(module, "@babelsuite/")
		return loadModuleDir(suffix, reg, resolve)
	}
	if strings.HasPrefix(module, "@plugins/") {
		suffix := strings.TrimPrefix(module, "@plugins/")
		return loadModuleDir(suffix, reg, resolve)
	}
	return nil, fmt.Errorf("unknown module %q", module)
}

// ModuleResolver resolves @babelsuite/<name> module files. When nil, the
// embedded stdlib is used as a fallback (topology display / tests). The
// execution path passes an OCI-backed resolver that pulls from the catalog.
type ModuleResolver func(name string) (map[string]string, error)

func loadModuleDir(suffix string, reg *starlarkRegistry, resolve ModuleResolver) (starlark.StringDict, error) {
	var moduleFS fs.FS
	if resolve != nil {
		files, err := resolve(suffix)
		if err == nil && len(files) > 0 {
			moduleFS = mapFS(files)
		}
	}
	if moduleFS == nil {
		mfs, err := stdlib.ModuleFS(suffix)
		if err != nil {
			return nil, fmt.Errorf("module @babelsuite/%s not found", suffix)
		}
		moduleFS = mfs
	}

	modulePredeclared := starlark.StringDict{"utils": buildUtilsModule()}

	var loader func(_ *starlark.Thread, mod string) (starlark.StringDict, error)
	loader = func(_ *starlark.Thread, mod string) (starlark.StringDict, error) {
		if mod == "@babelsuite/runtime" {
			return buildRuntimeModule(reg)
		}
		if strings.HasPrefix(mod, "@plugins/") {
			return loadModuleDir(strings.TrimPrefix(mod, "@plugins/"), reg, resolve)
		}
		content, err := fs.ReadFile(moduleFS, mod)
		if err != nil {
			return nil, fmt.Errorf("cannot load %q: %w", mod, err)
		}
		t := &starlark.Thread{Name: mod, Load: loader}
		t.SetMaxExecutionSteps(starkit.MaxSteps)
		return starlark.ExecFileOptions(starkit.FileOptions, t, mod, content, modulePredeclared)
	}

	entries, err := fs.ReadDir(moduleFS, ".")
	if err != nil {
		return nil, fmt.Errorf("module @babelsuite/%s not found", suffix)
	}

	combined := make(starlark.StringDict)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".star") || strings.HasPrefix(name, "_") || name == "module.star" || name == "usage.star" {
			continue
		}
		content, err := fs.ReadFile(moduleFS, name)
		if err != nil {
			continue
		}
		t := &starlark.Thread{Name: "@babelsuite/" + suffix + "/" + name, Load: loader}
		t.SetMaxExecutionSteps(starkit.MaxSteps)
		globals, err := starlark.ExecFileOptions(starkit.FileOptions, t, name, content, modulePredeclared)
		if err != nil {
			return nil, fmt.Errorf("module @babelsuite/%s: error in %s: %w", suffix, name, err)
		}
		for symName, val := range globals {
			if !strings.HasPrefix(symName, "_") {
				combined[symName] = val
			}
		}
	}
	return combined, nil
}

// mapFS wraps a map[string]string as an fs.FS backed by strings.Reader.
type mapFS map[string]string

func (m mapFS) Open(name string) (fs.File, error) {
	content, ok := m[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &mapFile{Reader: strings.NewReader(content)}, nil
}

type mapFile struct{ *strings.Reader }

func (f *mapFile) Close() error               { return nil }
func (f *mapFile) Stat() (fs.FileInfo, error) { return nil, fmt.Errorf("not supported") }

func (m mapFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if name != "." {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries := make([]fs.DirEntry, 0, len(m))
	for k := range m {
		entries = append(entries, mapDirEntry(k))
	}
	return entries, nil
}

type mapDirEntry string

func (e mapDirEntry) Name() string               { return string(e) }
func (e mapDirEntry) IsDir() bool                { return false }
func (e mapDirEntry) Type() fs.FileMode          { return 0 }
func (e mapDirEntry) Info() (fs.FileInfo, error) { return nil, fmt.Errorf("not supported") }

func buildRuntimeModule(reg *starlarkRegistry) (starlark.StringDict, error) {
	// service.run starts a container and keeps it running until the suite
	// ends; mock.api is a fake API the sidecar answers, with no container.
	service := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"run": buildServiceRunFunc(reg),
		},
		Renamed: map[string]string{"mock": "mock.api"},
	}
	mock := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"api": buildNodeFunc(reg, model.VariantMockAPI),
		},
	}
	task := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"run": buildNodeFunc(reg, "task.run"),
		},
	}
	test := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"run": buildNodeFunc(reg, "test.run"),
		},
	}
	trafficMethods := make(map[string]starkit.Builder, len(trafficVariants))
	for _, v := range trafficVariants {
		trafficMethods[v] = buildNodeFunc(reg, "traffic."+v)
	}
	traffic := &starkit.Namespace{Methods: trafficMethods}
	suite := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"run": buildNodeFunc(reg, "suite.run"),
		},
	}
	security := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"probe":   buildNodeFunc(reg, "security.probe"),
			"fuzz":    buildNodeFunc(reg, "security.fuzz"),
			"auth":    buildNodeFunc(reg, "security.auth"),
			"flood":   buildNodeFunc(reg, "security.flood"),
			"headers": buildNodeFunc(reg, "security.headers"),
			"verbs":   buildNodeFunc(reg, "security.verbs"),
			"graphql": buildNodeFunc(reg, "security.graphql"),
			"cors":    buildNodeFunc(reg, "security.cors"),
		},
	}
	log := &starkit.Namespace{
		Methods: map[string]starkit.Builder{
			"info":  buildNodeFunc(reg, "log.info"),
			"warn":  buildNodeFunc(reg, "log.warn"),
			"error": buildNodeFunc(reg, "log.error"),
			"debug": buildNodeFunc(reg, "log.debug"),
		},
	}

	plugin := starlark.NewBuiltin("plugin", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, fmt.Errorf("plugin() takes exactly one positional argument (the registered plugin name)")
		}
		name, ok := starlark.AsString(args[0])
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("plugin() argument must be a non-empty string")
		}
		return &starlarkPluginRef{reg: reg, pluginName: strings.TrimSpace(name)}, nil
	})

	return starlark.StringDict{
		"service":  service,
		"mock":     mock,
		"task":     task,
		"test":     test,
		"traffic":  traffic,
		"suite":    suite,
		"security": security,
		"log":      log,
		"plugin":   plugin,
	}, nil
}

// buildServiceRunFunc is service.run(...), which starts a container and keeps
// it running, so it needs image=. Without one it used to start nothing and
// report healthy anyway — a stand-in nobody could tell from a real service.
func buildServiceRunFunc(reg *starlarkRegistry) starkit.Builder {
	build := buildNodeFunc(reg, model.VariantServiceRun)
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		hasImage := false
		for _, kv := range kwargs {
			if key, _ := starlark.AsString(kv[0]); key == "image" {
				image, _ := starlark.AsString(kv[1])
				hasImage = strings.TrimSpace(image) != ""
			}
		}
		if !hasImage {
			return nil, fmt.Errorf("service.run needs image=: it starts that container and keeps it running — use mock.api for an API the suite only needs to fake")
		}
		return build(thread, fn, args, kwargs)
	}
}

func buildNodeFunc(reg *starlarkRegistry, variant string) starkit.Builder {
	declared, ok := stepKinds[variant]
	if !ok {
		panic("suites: no step kind declared for " + variant)
	}
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		node := &starlarkNode{
			stepCommon: stepCommon{kind: declared.kind, variant: variant},
			spec:       declared.newSpec(),
		}
		call := &stepCall{variant: variant, common: &node.stepCommon, spec: node.spec}

		if len(args) > 0 {
			if s, ok := starlark.AsString(args[0]); ok {
				node.message = strings.TrimSpace(s)
			}
		}

		for _, kwarg := range kwargs {
			key := string(kwarg[0].(starlark.String))
			// A keyword the call does not read used to be dropped, and the step
			// then ran without it: expect_throtle=True asked for a rate limit to
			// be required and required nothing.
			if !declared.allowed[key] {
				return nil, fmt.Errorf("%s: unknown argument %q; allowed: %s", variant, key, declared.keyList())
			}
			if err := stepArgs[key].apply(call, kwarg[1]); err != nil {
				return nil, err
			}
		}

		reg.register(node)
		return node, nil
	}
}

// starlarkPluginRef is returned by plugin("registered-plugin-name").
// It binds the plugin name and exposes a single .run() method.
type starlarkPluginRef struct {
	reg        *starlarkRegistry
	pluginName string
}

func (p *starlarkPluginRef) String() string       { return "plugin(" + p.pluginName + ")" }
func (p *starlarkPluginRef) Type() string         { return "babelsuite.PluginRef" }
func (p *starlarkPluginRef) Freeze()              {}
func (p *starlarkPluginRef) Truth() starlark.Bool { return starlark.True }
func (p *starlarkPluginRef) Hash() (uint32, error) {
	return 0, fmt.Errorf("plugin ref is not hashable")
}
func (p *starlarkPluginRef) AttrNames() []string {
	return []string{"run", "watch", "monitor", "check", "assert", "probe"}
}

// Attr returns a step builder for any operation name.
//
// A plugin call's keywords are of two kinds. The step options — after,
// on_failure, continue_on_failure, expect_logs and fail_on_logs — are the
// engine's: they decide how the step is ordered and judged, they mean the same
// on every node, and they never reach the plugin. Every other keyword is the
// plugin's own config. The operation name has no say in whether a step may fail
// the run: a check that only warns passes because its findings are warnings,
// and an author who wants a failing step not to stop the run says so with
// continue_on_failure=True.
func (p *starlarkPluginRef) Attr(op string) (starlark.Value, error) {
	return starlark.NewBuiltin(op, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		plugin := &pluginCall{op: op, config: make(map[string]any)}
		node := &starlarkNode{
			stepCommon: stepCommon{kind: model.NodeKindPlugin, variant: p.pluginName},
			spec:       plugin,
		}
		var expectLogs, failOnLogs []string
		call := "plugin." + op
		for _, kv := range kwargs {
			key, _ := starlark.AsString(kv[0])
			val := kv[1]
			switch key {
			case "name":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("plugin.%s: name must be a string", op)
				}
				node.name = strings.TrimSpace(s)
				node.explicitName = node.name != ""
			case "after":
				nodes, err := extractNodeList(val, call, "after")
				if err != nil {
					return nil, err
				}
				node.after = nodes
			case "on_failure":
				nodes, err := extractNodeList(val, call, "on_failure")
				if err != nil {
					return nil, err
				}
				node.onFailure = nodes
			case "continue_on_failure":
				b, ok := val.(starlark.Bool)
				if !ok {
					return nil, fmt.Errorf("%s: continue_on_failure must be a bool", call)
				}
				node.continueOnFail = bool(b)
			case "expect_logs":
				matchers, err := extractStringOrList(val, call, "expect_logs")
				if err != nil {
					return nil, err
				}
				expectLogs = matchers
			case "fail_on_logs":
				matchers, err := extractStringOrList(val, call, "fail_on_logs")
				if err != nil {
					return nil, err
				}
				failOnLogs = matchers
			case "expect_exit":
				return nil, fmt.Errorf("%s: expect_exit does not apply to a plugin step, which has no exit code; use expect_logs or fail_on_logs", call)
			default:
				plugin.config[key] = starlarkToGo(val)
			}
		}
		if node.name == "" {
			return nil, fmt.Errorf("plugin.%s: name is required", op)
		}
		if len(expectLogs) > 0 || len(failOnLogs) > 0 {
			plugin.evaluation = &model.StepEvaluation{ExpectLogs: expectLogs, FailOnLogs: failOnLogs}
		}
		p.reg.register(node)
		return node, nil
	}), nil
}

func extractNodeList(val starlark.Value, call, param string) ([]*starlarkNode, error) {
	list, ok := val.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a list", call, param)
	}
	out := make([]*starlarkNode, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		elem := list.Index(i)
		node, ok := elem.(*starlarkNode)
		if !ok {
			return nil, fmt.Errorf("%s: %s elements must be node references", call, param)
		}
		out = append(out, node)
	}
	return out, nil
}

func extractArtifactExports(val starlark.Value, call string) ([]model.ArtifactExport, error) {
	list, ok := val.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: exports must be a list of dicts", call)
	}
	out := make([]model.ArtifactExport, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		d, ok := list.Index(i).(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("%s: exports elements must be dicts", call)
		}
		export := model.ArtifactExport{On: "success"}
		enforceSet := false
		for _, item := range d.Items() {
			k, ok := starlark.AsString(item[0])
			if !ok {
				continue
			}
			switch k {
			case "path":
				s, ok := starlark.AsString(item[1])
				if !ok {
					return nil, fmt.Errorf("%s: exports path must be a string", call)
				}
				export.Path = strings.TrimSpace(s)
			case "name":
				s, ok := starlark.AsString(item[1])
				if !ok {
					return nil, fmt.Errorf("%s: exports name must be a string", call)
				}
				export.Name = strings.TrimSpace(s)
			case "on":
				s, ok := starlark.AsString(item[1])
				if !ok {
					return nil, fmt.Errorf("%s: exports on must be a string", call)
				}
				export.On = strings.TrimSpace(s)
			case "format":
				s, ok := starlark.AsString(item[1])
				if !ok {
					return nil, fmt.Errorf("%s: exports format must be a string", call)
				}
				export.Format = strings.ToLower(strings.TrimSpace(s))
			case "enforce":
				b, ok := item[1].(starlark.Bool)
				if !ok {
					return nil, fmt.Errorf("%s: exports enforce must be True or False", call)
				}
				export.NotEnforced = !bool(b)
				enforceSet = true
			default:
				return nil, fmt.Errorf("%s: exports entry has an unknown key %q; use path, name, on, format or enforce", call, k)
			}
		}
		if strings.TrimSpace(export.Path) == "" {
			return nil, fmt.Errorf("%s: exports entry missing required path", call)
		}
		switch export.Format {
		case "", "junit", "ctrf", "cobertura":
		default:
			return nil, fmt.Errorf("%s: exports format %q is not supported; use junit, ctrf or cobertura", call, export.Format)
		}
		if enforceSet && export.Format != "junit" && export.Format != "ctrf" {
			return nil, fmt.Errorf("%s: exports enforce applies only to a junit or ctrf export", call)
		}
		switch export.On {
		case "", "success":
			export.On = "success"
		case "failure", "always":
		default:
			return nil, fmt.Errorf("%s: exports unsupported trigger %q", call, export.On)
		}
		out = append(out, export)
	}
	return out, nil
}

func extractStringOrList(val starlark.Value, call, param string) ([]string, error) {
	if s, ok := starlark.AsString(val); ok {
		if s != "" {
			return []string{s}, nil
		}
		return nil, nil
	}
	list, ok := val.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a string or list of strings", call, param)
	}
	out := make([]string, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		s, ok := starlark.AsString(list.Index(i))
		if !ok {
			return nil, fmt.Errorf("%s: %s list elements must be strings", call, param)
		}
		if s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func assignIDs(reg *starlarkRegistry, globals starlark.StringDict) {
	nodeToVar := make(map[*starlarkNode]string, len(reg.nodes))
	for varName, val := range globals {
		if node, ok := val.(*starlarkNode); ok {
			if existing, seen := nodeToVar[node]; !seen || len(varName) > len(existing) || (len(varName) == len(existing) && varName < existing) {
				nodeToVar[node] = varName
			}
		}
	}

	for _, node := range reg.nodes {
		varName := nodeToVar[node]

		if node.explicitName {
			node.id = node.name
		} else if varName != "" {
			node.id = varName
			node.name = varName
		} else {
			node.id = fmt.Sprintf("node_%d", node.order)
			node.name = node.id
		}
	}
}

func starlarkToGo(val starlark.Value) any {
	switch v := val.(type) {
	case starlark.String:
		s, _ := starlark.AsString(val)
		return s
	case starlark.Int:
		n, _ := v.Int64()
		return n
	case starlark.Float:
		f, _ := starlark.AsFloat(val)
		return f
	case starlark.Bool:
		return bool(v)
	case *starlark.List:
		out := make([]any, v.Len())
		for i := range v.Len() {
			out[i] = starlarkToGo(v.Index(i))
		}
		return out
	case starlark.NoneType:
		return nil
	default:
		return val.String()
	}
}

func buildRawNodes(reg *starlarkRegistry) []RawNode {
	raw := make([]RawNode, 0, len(reg.nodes))
	for _, node := range reg.nodes {
		rn := RawNode{
			Assignment:        node.id,
			ID:                node.id,
			Name:              node.name,
			Kind:              node.kind,
			Variant:           node.variant,
			Message:           node.message,
			ContinueOnFailure: node.continueOnFail,
			Order:             node.order,
		}
		if node.spec != nil {
			node.spec.fill(&rn)
		}

		for _, dep := range node.after {
			if dep.id != "" {
				rn.DependsOn = append(rn.DependsOn, dep.id)
			}
		}
		for _, dep := range node.onFailure {
			if dep.id != "" {
				rn.OnFailure = append(rn.OnFailure, dep.id)
			}
		}

		raw = append(raw, rn)
	}
	return raw
}

// RawNode is a step as the evaluator read it, before the resolver orders it and
// expands imports.
type RawNode struct {
	Assignment        string
	ID                string
	Name              string
	Kind              string
	Variant           string
	Image             string
	File              string
	Commands          []string
	Ref               string
	Target            string
	Plan              string
	RPS               float64
	ArrivalRate       float64
	MinUsers          float64
	Technique         string
	FloodPath         string
	FloodRate         float64
	FloodDuration     float64
	FloodThrottle     bool
	FailOn            string
	Message           string
	Harden            bool
	DependsOn         []string
	ResetMocks        []string
	OnFailure         []string
	ContinueOnFailure bool
	Evaluation        *model.StepEvaluation
	Exports           []model.ArtifactExport
	Env               map[string]string
	PluginOp          string
	PluginConfig      map[string]any
	Order             int
}
