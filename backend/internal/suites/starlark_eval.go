package suites

import (
	"fmt"
	"io/fs"
	"strings"
	"sync"

	"github.com/babelsuite/babelsuite/internal/stdlib"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

const starlarkMaxSteps uint64 = 1_000_000

var starlarkFileOptions = &syntax.FileOptions{
	Set:             true,
	GlobalReassign:  true,
	Recursion:       false,
	TopLevelControl: true,
	While:           false,
}

type starlarkNode struct {
	id              string
	name            string
	explicitName    bool
	kind            string
	variant         string
	image    string
	file     string
	commands []string
	ref      string
	plan            string
	target          string
	technique       string
	floodPath       string
	floodRate       float64
	floodDuration   float64
	floodThrottle   bool
	rps             float64
	arrivalRate     float64
	message         string
	after           []*starlarkNode
	resetMocks      []*starlarkNode
	onFailure       []*starlarkNode
	continueOnFail  bool
	pluginOp        string
	pluginConfig    map[string]any
	evaluation      *StepEvaluation
	exports         []ArtifactExport
	env             map[string]string
	order           int
}

func (n *starlarkNode) String() string        { return n.name }
func (n *starlarkNode) Type() string          { return "babelsuite.Node" }
func (n *starlarkNode) Freeze()               {}
func (n *starlarkNode) Truth() starlark.Bool  { return starlark.True }
func (n *starlarkNode) Hash() (uint32, error) { return 0, fmt.Errorf("node is not hashable") }
func (n *starlarkNode) AttrNames() []string   { return []string{"name"} }
func (n *starlarkNode) Attr(attr string) (starlark.Value, error) {
	if attr == "name" {
		return starlark.String(n.name), nil
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

func evalStarlarkTopology(suiteStar string, resolve ModuleResolver) (nodes []rawTopologyNode, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			retErr = fmt.Errorf("starlark evaluation panicked: %v", r)
		}
	}()

	reg := &starlarkRegistry{}

	predeclared, err := buildRuntimePredeclared(reg)
	if err != nil {
		return nil, err
	}

	thread := &starlark.Thread{
		Name: "suite.star",
		Load: func(t *starlark.Thread, module string) (starlark.StringDict, error) {
			return resolveStarlarkModule(module, reg, resolve)
		},
	}
	thread.SetMaxExecutionSteps(starlarkMaxSteps)

	globals, err := starlark.ExecFileOptions(starlarkFileOptions, thread, "suite.star", suiteStar, predeclared)
	if err != nil {
		return nil, fmt.Errorf("starlark: %w", err)
	}

	if len(reg.nodes) == 0 {
		return nil, fmt.Errorf("starlark: no topology nodes registered")
	}

	assignIDs(reg, globals)
	return buildRawNodes(reg), nil
}

func buildRuntimePredeclared(reg *starlarkRegistry) (starlark.StringDict, error) {
	runtimeModule, err := buildRuntimeModule(reg)
	if err != nil {
		return nil, err
	}
	return starlark.StringDict{
		"service":  runtimeModule["service"],
		"task":     runtimeModule["task"],
		"test":     runtimeModule["test"],
		"traffic":  runtimeModule["traffic"],
		"suite":    runtimeModule["suite"],
		"security": runtimeModule["security"],
		"log":      runtimeModule["log"],
		"plugin":   runtimeModule["plugin"],
		"env":      frozenEmptyDict(),
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
		content, err := fs.ReadFile(moduleFS, mod)
		if err != nil {
			return nil, fmt.Errorf("cannot load %q: %w", mod, err)
		}
		t := &starlark.Thread{Name: mod, Load: loader}
		t.SetMaxExecutionSteps(starlarkMaxSteps)
		return starlark.ExecFileOptions(starlarkFileOptions, t, mod, content, modulePredeclared)
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
		t.SetMaxExecutionSteps(starlarkMaxSteps)
		globals, err := starlark.ExecFileOptions(starlarkFileOptions, t, name, content, modulePredeclared)
		if err != nil {
			continue
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
	service := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
			"run":  buildNodeFunc(reg, "service.run"),
			"mock": buildNodeFunc(reg, "service.mock"),
		},
	}
	task := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
			"run": buildNodeFunc(reg, "task.run"),
		},
	}
	test := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
			"run": buildNodeFunc(reg, "test.run"),
		},
	}
	trafficVariants := []string{
		"smoke", "baseline", "stress", "spike", "soak",
		"scalability", "step", "wave", "staged",
		"constant_throughput", "constant_pacing", "open_model",
	}
	trafficMethods := make(map[string]starlarkBuilderFunc, len(trafficVariants))
	for _, v := range trafficVariants {
		trafficMethods[v] = buildNodeFunc(reg, "traffic."+v)
	}
	traffic := &starlarkNamespace{reg: reg, methods: trafficMethods}
	suite := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
			"run": buildNodeFunc(reg, "suite.run"),
		},
	}
	security := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
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
	log := &starlarkNamespace{
		reg: reg,
		methods: map[string]starlarkBuilderFunc{
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
		"task":     task,
		"test":     test,
		"traffic":  traffic,
		"suite":    suite,
		"security": security,
		"log":      log,
		"plugin":   plugin,
	}, nil
}

type starlarkBuilderFunc func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error)

type starlarkNamespace struct {
	reg     *starlarkRegistry
	methods map[string]starlarkBuilderFunc
}

func (ns *starlarkNamespace) String() string        { return "babelsuite.Namespace" }
func (ns *starlarkNamespace) Type() string          { return "babelsuite.Namespace" }
func (ns *starlarkNamespace) Freeze()               {}
func (ns *starlarkNamespace) Truth() starlark.Bool  { return starlark.True }
func (ns *starlarkNamespace) Hash() (uint32, error) { return 0, fmt.Errorf("namespace is not hashable") }

func (ns *starlarkNamespace) Attr(name string) (starlark.Value, error) {
	fn, ok := ns.methods[name]
	if !ok {
		return nil, nil
	}
	return starlark.NewBuiltin(name, fn), nil
}

func (ns *starlarkNamespace) AttrNames() []string {
	names := make([]string, 0, len(ns.methods))
	for k := range ns.methods {
		names = append(names, k)
	}
	return names
}

func buildNodeFunc(reg *starlarkRegistry, variant string) starlarkBuilderFunc {
	kind, _ := topologyKind(variant)
	if kind == "" {
		kind = variant
	}
	return func(thread *starlark.Thread, fn *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		node := &starlarkNode{
			kind:    kind,
			variant: variant,
		}

		if len(args) > 0 {
			if s, ok := starlark.AsString(args[0]); ok {
				node.message = strings.TrimSpace(s)
			}
		}

		var expectExit *int
		var expectLogs []string
		var failOnLogs []string

		for _, kwarg := range kwargs {
			key := string(kwarg[0].(starlark.String))
			val := kwarg[1]

			switch key {
			case "name", "name_or_id", "id":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: %s must be a string", variant, key)
				}
				node.name = strings.TrimSpace(s)
				node.explicitName = true

			case "message":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: message must be a string", variant)
				}
				node.message = strings.TrimSpace(s)

			case "image":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: image must be a string", variant)
				}
				node.image = strings.TrimSpace(s)

			case "file":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: file must be a string", variant)
				}
				node.file = strings.TrimSpace(s)

			case "commands":
				list, ok := val.(*starlark.List)
				if !ok {
					return nil, fmt.Errorf("%s: commands must be a list of strings", variant)
				}
				cmds := make([]string, list.Len())
				for i := range list.Len() {
					s, ok := starlark.AsString(list.Index(i))
					if !ok {
						return nil, fmt.Errorf("%s: commands entries must be strings", variant)
					}
					cmds[i] = s
				}
				node.commands = cmds

			case "ref":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: ref must be a string", variant)
				}
				node.ref = strings.TrimSpace(s)

			case "after":
				deps, err := extractNodeList(val, variant, "after")
				if err != nil {
					return nil, err
				}
				node.after = deps

			case "reset_mocks":
				deps, err := extractNodeList(val, variant, "reset_mocks")
				if err != nil {
					return nil, err
				}
				node.resetMocks = deps

			case "on_failure":
				deps, err := extractNodeList(val, variant, "on_failure")
				if err != nil {
					return nil, err
				}
				node.onFailure = deps

			case "continue_on_failure":
				b, ok := val.(starlark.Bool)
				if !ok {
					return nil, fmt.Errorf("%s: continue_on_failure must be a bool", variant)
				}
				node.continueOnFail = bool(b)

			case "plan":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: plan must be a string", variant)
				}
				node.plan = strings.TrimSpace(s)

			case "target":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: target must be a string", variant)
				}
				node.target = strings.TrimSpace(s)

			case "rps", "target_rps":
				if f, ok := starlark.AsFloat(val); ok {
					node.rps = f
				}

			case "arrival_rate":
				if f, ok := starlark.AsFloat(val); ok {
					node.arrivalRate = f
				}

			case "expect_exit":
				code, ok := val.(starlark.Int)
				if !ok {
					return nil, fmt.Errorf("%s: expect_exit must be an int", variant)
				}
				n64, _ := code.Int64()
				n := int(n64)
				expectExit = &n

			case "expect_logs":
				matchers, err := extractStringOrList(val, variant, "expect_logs")
				if err != nil {
					return nil, err
				}
				expectLogs = matchers

			case "fail_on_logs":
				matchers, err := extractStringOrList(val, variant, "fail_on_logs")
				if err != nil {
					return nil, err
				}
				failOnLogs = matchers

			case "exports":
				exports, err := extractArtifactExports(val, variant)
				if err != nil {
					return nil, err
				}
				node.exports = exports

			case "env":
				d, ok := val.(*starlark.Dict)
				if !ok {
					return nil, fmt.Errorf("%s: env must be a dict", variant)
				}
				envMap := make(map[string]string, d.Len())
				for _, kv := range d.Items() {
					k, ok1 := starlark.AsString(kv[0])
					v, ok2 := starlark.AsString(kv[1])
					if ok1 && ok2 {
						envMap[k] = v
					}
				}
				node.env = envMap

			case "technique":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: technique must be a string", variant)
				}
				node.technique = strings.TrimSpace(s)

			case "path":
				s, ok := starlark.AsString(val)
				if !ok {
					return nil, fmt.Errorf("%s: path must be a string", variant)
				}
				node.floodPath = strings.TrimSpace(s)

			case "rate":
				if f, ok := starlark.AsFloat(val); ok {
					node.floodRate = f
				}

			case "duration":
				if f, ok := starlark.AsFloat(val); ok {
					node.floodDuration = f
				}

			case "expect_throttle":
				b, ok := val.(starlark.Bool)
				if !ok {
					return nil, fmt.Errorf("%s: expect_throttle must be a bool", variant)
				}
				node.floodThrottle = bool(b)
			}
		}

		if expectExit != nil || len(expectLogs) > 0 || len(failOnLogs) > 0 {
			node.evaluation = &StepEvaluation{
				ExpectExit: expectExit,
				ExpectLogs: expectLogs,
				FailOnLogs: failOnLogs,
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

func (p *starlarkPluginRef) String() string        { return "plugin(" + p.pluginName + ")" }
func (p *starlarkPluginRef) Type() string          { return "babelsuite.PluginRef" }
func (p *starlarkPluginRef) Freeze()               {}
func (p *starlarkPluginRef) Truth() starlark.Bool  { return starlark.True }
func (p *starlarkPluginRef) Hash() (uint32, error) { return 0, fmt.Errorf("plugin ref is not hashable") }
func (p *starlarkPluginRef) AttrNames() []string {
	return []string{"run", "watch", "monitor", "check", "assert", "probe"}
}

// Attr returns a step builder for any operation name. The operation name controls
// failure behaviour: watch/monitor are non-blocking (continue_on_failure=true),
// all others are blocking and fail the suite on findings.
func (p *starlarkPluginRef) Attr(op string) (starlark.Value, error) {
	softFail := op == "watch" || op == "monitor"
	return starlark.NewBuiltin(op, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		node := &starlarkNode{
			kind:           NodeKindPlugin,
			variant:        p.pluginName,
			pluginOp:       op,
			continueOnFail: softFail,
			pluginConfig:   make(map[string]any),
		}
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
				nodes, err := extractNodeList(val, "plugin."+op, "after")
				if err != nil {
					return nil, err
				}
				node.after = nodes
			default:
				node.pluginConfig[key] = starlarkToGo(val)
			}
		}
		if node.name == "" {
			return nil, fmt.Errorf("plugin.%s: name is required", op)
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

func extractArtifactExports(val starlark.Value, call string) ([]ArtifactExport, error) {
	list, ok := val.(*starlark.List)
	if !ok {
		return nil, fmt.Errorf("%s: exports must be a list of dicts", call)
	}
	out := make([]ArtifactExport, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		d, ok := list.Index(i).(*starlark.Dict)
		if !ok {
			return nil, fmt.Errorf("%s: exports elements must be dicts", call)
		}
		export := ArtifactExport{On: "success"}
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
			}
		}
		if strings.TrimSpace(export.Path) == "" {
			return nil, fmt.Errorf("%s: exports entry missing required path", call)
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

func buildStarlarkArguments(node *starlarkNode) string {
	var parts []string
	if node.plan != "" {
		parts = append(parts, fmt.Sprintf(`plan="%s"`, node.plan))
	}
	if node.target != "" {
		parts = append(parts, fmt.Sprintf(`target="%s"`, node.target))
	}
	if node.rps > 0 {
		parts = append(parts, fmt.Sprintf(`rps=%g`, node.rps))
	}
	if node.arrivalRate > 0 {
		parts = append(parts, fmt.Sprintf(`arrival_rate=%g`, node.arrivalRate))
	}
	if node.technique != "" {
		parts = append(parts, fmt.Sprintf(`technique="%s"`, node.technique))
	}
	if node.floodPath != "" {
		parts = append(parts, fmt.Sprintf(`path="%s"`, node.floodPath))
	}
	if node.floodRate > 0 {
		parts = append(parts, fmt.Sprintf(`rate=%g`, node.floodRate))
	}
	if node.floodDuration > 0 {
		parts = append(parts, fmt.Sprintf(`duration=%g`, node.floodDuration))
	}
	if node.floodThrottle {
		parts = append(parts, `expect_throttle=True`)
	}
	return strings.Join(parts, ", ")
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

func frozenEmptyDict() *starlark.Dict {
	d := starlark.NewDict(0)
	d.Freeze()
	return d
}

func buildRawNodes(reg *starlarkRegistry) []rawTopologyNode {
	raw := make([]rawTopologyNode, 0, len(reg.nodes))
	for _, node := range reg.nodes {
		rn := rawTopologyNode{
			Assignment:        node.id,
			ID:                node.id,
			Name:              node.name,
			Kind:              node.kind,
			Variant:           node.variant,
			Image:             node.image,
			File:              node.file,
			Commands:          append([]string{}, node.commands...),
			Ref:               node.ref,
			Target:            node.target,
			Technique:         node.technique,
			FloodPath:         node.floodPath,
			FloodRate:         node.floodRate,
			FloodDuration:     node.floodDuration,
			FloodThrottle:     node.floodThrottle,
			Message:           node.message,
			Arguments:         buildStarlarkArguments(node),
			ContinueOnFailure: node.continueOnFail,
			Evaluation:        node.evaluation,
			Exports:           append([]ArtifactExport{}, node.exports...),
			Env:               cloneStringMap(node.env),
			PluginOp:          node.pluginOp,
			PluginConfig:      node.pluginConfig,
			Order:             node.order,
		}

		for _, dep := range node.after {
			if dep.id != "" {
				rn.DependsOn = append(rn.DependsOn, dep.id)
			}
		}
		for _, dep := range node.resetMocks {
			if dep.id != "" {
				rn.ResetMocks = append(rn.ResetMocks, dep.id)
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
