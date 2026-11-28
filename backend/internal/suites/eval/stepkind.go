package eval

import (
	"fmt"
	"github.com/babelsuite/babelsuite/internal/suites/model"
	"sort"
	"strings"

	"go.starlark.net/starlark"
)

// Every step kind is declared once, here: which keywords its call accepts and
// what it keeps of them. The evaluator unpacks a call from this declaration, so
// the keyword check, the text of its error, and the keyword tables in the docs
// cannot disagree with what a call reads. (Before, a 280-line switch read the
// keywords and two key tables said which were allowed.)

// stepCommon is what every node has, whatever its kind.
type stepCommon struct {
	id             string
	name           string
	explicitName   bool
	kind           string
	variant        string
	message        string
	after          []*starlarkNode
	onFailure      []*starlarkNode
	continueOnFail bool
	order          int
}

// stepSpec is what a node keeps that is particular to its kind.
type stepSpec interface {
	// fill copies the spec into the topology node the resolver works on.
	fill(raw *RawNode)
}

// runSpec is what a step that runs something is judged by.
type runSpec struct {
	env        map[string]string
	exports    []model.ArtifactExport
	expectExit *int
	expectLogs []string
	failOnLogs []string
}

func (r *runSpec) runPart() *runSpec { return r }

func (r *runSpec) evaluation() *model.StepEvaluation {
	if r.expectExit == nil && len(r.expectLogs) == 0 && len(r.failOnLogs) == 0 {
		return nil
	}
	return &model.StepEvaluation{ExpectExit: r.expectExit, ExpectLogs: r.expectLogs, FailOnLogs: r.failOnLogs}
}

func (r *runSpec) fillRun(raw *RawNode) {
	raw.Evaluation = r.evaluation()
	raw.Exports = append([]model.ArtifactExport{}, r.exports...)
	raw.Env = cloneStringMap(r.env)
}

// execSpec is service.run, task.run and test.run.
type execSpec struct {
	runSpec
	image      string
	file       string
	commands   []string
	resetMocks []*starlarkNode
}

func (s *execSpec) fill(raw *RawNode) {
	raw.Image = s.image
	raw.File = s.file
	raw.Commands = append([]string{}, s.commands...)
	s.fillRun(raw)
	for _, dep := range s.resetMocks {
		if dep.id != "" {
			raw.ResetMocks = append(raw.ResetMocks, dep.id)
		}
	}
}

// mockSpec is mock.api.
type mockSpec struct{ harden bool }

func (s *mockSpec) fill(raw *RawNode) { raw.Harden = s.harden }

// suiteRefSpec is suite.run.
type suiteRefSpec struct{ ref string }

func (s *suiteRefSpec) fill(raw *RawNode) { raw.Ref = s.ref }

// trafficArgs is every traffic.* call.
type trafficArgs struct {
	runSpec
	plan        string
	target      string
	rps         float64
	arrivalRate float64
	minUsers    float64
}

func (s *trafficArgs) fill(raw *RawNode) {
	raw.Target = s.target
	raw.Plan = s.plan
	raw.RPS = s.rps
	raw.ArrivalRate = s.arrivalRate
	raw.MinUsers = s.minUsers
	s.fillRun(raw)
}

// securityArgs is every security.* call.
type securityArgs struct {
	runSpec
	target        string
	technique     string
	floodPath     string
	floodRate     float64
	floodDuration float64
	floodThrottle bool
	failOn        string
}

func (s *securityArgs) fill(raw *RawNode) {
	raw.Target = s.target
	raw.Technique = s.technique
	raw.FloodPath = s.floodPath
	raw.FloodRate = s.floodRate
	raw.FloodDuration = s.floodDuration
	raw.FloodThrottle = s.floodThrottle
	raw.FailOn = s.failOn
	s.fillRun(raw)
}

// logSpec is log.*: a message, which every step has.
type logSpec struct{}

func (s *logSpec) fill(*RawNode) {}

// pluginCall is a plugin step: an operation and an open-ended config, checked
// against the plugin's schema when the step runs.
type pluginCall struct {
	op         string
	config     map[string]any
	evaluation *model.StepEvaluation
}

func (s *pluginCall) fill(raw *RawNode) {
	raw.PluginOp = s.op
	raw.PluginConfig = s.config
	raw.Evaluation = s.evaluation
}

// runner is a spec with run settings.
type runner interface{ runPart() *runSpec }

// argSpec is one keyword: what it is called and how its value is read.
type argSpec struct {
	name  string
	alias bool // a second spelling of another keyword; left out of error lists
	apply func(call *stepCall, v starlark.Value) error
}

// stepCall is a call being unpacked: the common fields, the kind's spec, and the
// variant for error messages.
type stepCall struct {
	variant string
	common  *stepCommon
	spec    stepSpec
}

// stepKind is one call a suite can make.
type stepKind struct {
	variant string
	kind    string
	newSpec func() stepSpec
	args    []string
	allowed map[string]bool
}

// orderingKeys are accepted by every call: a name, a message and how the step
// is ordered.
var orderingKeys = []string{"name", "name_or_id", "id", "message", "after", "on_failure", "continue_on_failure"}

// runKeys are what a step that runs something is judged by.
var runKeys = []string{"env", "exports", "expect_exit", "expect_logs", "fail_on_logs"}

func joinKeys(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

var (
	trafficVariants  = []string{"smoke", "baseline", "stress", "spike", "soak", "scalability", "step", "wave", "staged", "constant_throughput", "constant_pacing", "open_model"}
	securityVariants = []string{"probe", "fuzz", "auth", "flood", "headers", "verbs", "graphql", "cors"}
	logVariants      = []string{"info", "warn", "error", "debug"}
)

// stepKinds is the declaration of every call. Plugin steps are built by
// plugin(...).<op>(...), which has no fixed keywords.
var stepKinds = buildStepKinds()

func buildStepKinds() map[string]*stepKind {
	all := map[string]*stepKind{}
	add := func(variant, kind string, newSpec func() stepSpec, args ...string) {
		k := &stepKind{variant: variant, kind: kind, newSpec: newSpec, allowed: map[string]bool{}}
		for _, name := range joinKeys(orderingKeys, args) {
			k.allowed[name] = true
			k.args = append(k.args, name)
		}
		all[variant] = k
	}
	exec := func() stepSpec { return &execSpec{} }
	add(model.VariantServiceRun, "service", exec, joinKeys([]string{"image", "file", "commands"}, runKeys)...)
	add("task.run", "task", exec, joinKeys([]string{"image", "file", "commands", "reset_mocks"}, runKeys)...)
	add("test.run", "test", exec, joinKeys([]string{"image", "file", "commands", "reset_mocks"}, runKeys)...)
	add(model.VariantMockAPI, "mock", func() stepSpec { return &mockSpec{} }, "harden")
	add("suite.run", "suite", func() stepSpec { return &suiteRefSpec{} }, "ref")
	for _, v := range trafficVariants {
		add("traffic."+v, "traffic", func() stepSpec { return &trafficArgs{} },
			joinKeys([]string{"plan", "target", "rps", "target_rps", "arrival_rate", "min_users"}, runKeys)...)
	}
	for _, v := range securityVariants {
		extra := []string{}
		switch v {
		case "fuzz":
			extra = []string{"technique"}
		case "flood":
			extra = []string{"path", "rate", "duration", "expect_throttle"}
		}
		add("security."+v, model.NodeKindSecurity, func() stepSpec { return &securityArgs{} },
			joinKeys(runKeys, []string{"target", "fail_on"}, extra)...)
	}
	for _, v := range logVariants {
		add("log."+v, "log", func() stepSpec { return &logSpec{} })
	}
	return all
}

// keyList names the keywords a call accepts, for the error that rejects one it
// does not. name_or_id and id are aliases of name and are left out.
func (k *stepKind) keyList() string {
	seen := map[string]bool{}
	var out []string
	for _, name := range k.args {
		if arg, ok := stepArgs[name]; ok && arg.alias || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func stringArg(call *stepCall, name string, v starlark.Value) (string, error) {
	s, ok := starlark.AsString(v)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", call.variant, name)
	}
	return strings.TrimSpace(s), nil
}

func boolArg(call *stepCall, name string, v starlark.Value) (bool, error) {
	b, ok := v.(starlark.Bool)
	if !ok {
		return false, fmt.Errorf("%s: %s must be a bool", call.variant, name)
	}
	return bool(b), nil
}

func specAs[T any](call *stepCall, name string) (T, error) {
	spec, ok := call.spec.(T)
	if !ok {
		var zero T
		return zero, fmt.Errorf("%s: %s does not apply to this call", call.variant, name)
	}
	return spec, nil
}

func runOf(call *stepCall, name string) (*runSpec, error) {
	r, err := specAs[runner](call, name)
	if err != nil {
		return nil, err
	}
	return r.runPart(), nil
}

// stepArgs is how each keyword is read. A keyword is accepted by a call only if
// the call's declaration lists it.
var stepArgs = map[string]argSpec{}

func init() {
	def := func(apply func(call *stepCall, v starlark.Value) error, names ...string) {
		for _, name := range names {
			stepArgs[name] = argSpec{name: name, apply: apply}
		}
	}
	setName := func(name string) func(call *stepCall, v starlark.Value) error {
		return func(call *stepCall, v starlark.Value) error {
			s, err := stringArg(call, name, v)
			if err != nil {
				return err
			}
			call.common.name = s
			call.common.explicitName = true
			return nil
		}
	}
	def(setName("name"), "name")
	stepArgs["name_or_id"] = argSpec{name: "name_or_id", alias: true, apply: setName("name_or_id")}
	stepArgs["id"] = argSpec{name: "id", alias: true, apply: setName("id")}
	def(func(call *stepCall, v starlark.Value) error {
		s, err := stringArg(call, "message", v)
		call.common.message = s
		return err
	}, "message")
	def(func(call *stepCall, v starlark.Value) error {
		deps, err := extractNodeList(v, call.variant, "after")
		call.common.after = deps
		return err
	}, "after")
	def(func(call *stepCall, v starlark.Value) error {
		deps, err := extractNodeList(v, call.variant, "on_failure")
		call.common.onFailure = deps
		return err
	}, "on_failure")
	def(func(call *stepCall, v starlark.Value) error {
		b, err := boolArg(call, "continue_on_failure", v)
		call.common.continueOnFail = b
		return err
	}, "continue_on_failure")

	// exec: service.run, task.run, test.run
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*execSpec](call, "image")
		if err != nil {
			return err
		}
		s.image, err = stringArg(call, "image", v)
		return err
	}, "image")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*execSpec](call, "file")
		if err != nil {
			return err
		}
		s.file, err = stringArg(call, "file", v)
		return err
	}, "file")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*execSpec](call, "commands")
		if err != nil {
			return err
		}
		list, ok := v.(*starlark.List)
		if !ok {
			return fmt.Errorf("%s: commands must be a list of strings", call.variant)
		}
		cmds := make([]string, list.Len())
		for i := range list.Len() {
			str, ok := starlark.AsString(list.Index(i))
			if !ok {
				return fmt.Errorf("%s: commands entries must be strings", call.variant)
			}
			cmds[i] = str
		}
		s.commands = cmds
		return nil
	}, "commands")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*execSpec](call, "reset_mocks")
		if err != nil {
			return err
		}
		s.resetMocks, err = extractNodeList(v, call.variant, "reset_mocks")
		return err
	}, "reset_mocks")

	// mock.api, suite.run
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*mockSpec](call, "harden")
		if err != nil {
			return err
		}
		s.harden, err = boolArg(call, "harden", v)
		return err
	}, "harden")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*suiteRefSpec](call, "ref")
		if err != nil {
			return err
		}
		s.ref, err = stringArg(call, "ref", v)
		return err
	}, "ref")

	// traffic.*
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*trafficArgs](call, "plan")
		if err != nil {
			return err
		}
		s.plan, err = stringArg(call, "plan", v)
		return err
	}, "plan")
	def(func(call *stepCall, v starlark.Value) error {
		str, err := stringArg(call, "target", v)
		if err != nil {
			return err
		}
		switch s := call.spec.(type) {
		case *trafficArgs:
			s.target = str
		case *securityArgs:
			s.target = str
		default:
			return fmt.Errorf("%s: target does not apply to this call", call.variant)
		}
		return nil
	}, "target")
	number := func(name string, set func(s *trafficArgs, f float64), strict bool) func(call *stepCall, v starlark.Value) error {
		return func(call *stepCall, v starlark.Value) error {
			s, err := specAs[*trafficArgs](call, name)
			if err != nil {
				return err
			}
			f, ok := starlark.AsFloat(v)
			if !ok {
				if strict {
					return fmt.Errorf("%s: %s must be a number", call.variant, name)
				}
				return nil
			}
			set(s, f)
			return nil
		}
	}
	setRPS := number("rps", func(s *trafficArgs, f float64) { s.rps = f }, false)
	def(setRPS, "rps")
	stepArgs["target_rps"] = argSpec{name: "target_rps", apply: setRPS}
	def(number("arrival_rate", func(s *trafficArgs, f float64) { s.arrivalRate = f }, false), "arrival_rate")
	def(number("min_users", func(s *trafficArgs, f float64) { s.minUsers = f }, true), "min_users")

	// what a step that runs something is judged by
	def(func(call *stepCall, v starlark.Value) error {
		r, err := runOf(call, "env")
		if err != nil {
			return err
		}
		d, ok := v.(*starlark.Dict)
		if !ok {
			return fmt.Errorf("%s: env must be a dict", call.variant)
		}
		env := make(map[string]string, d.Len())
		for _, kv := range d.Items() {
			k, ok1 := starlark.AsString(kv[0])
			val, ok2 := starlark.AsString(kv[1])
			if ok1 && ok2 {
				env[k] = val
			}
		}
		r.env = env
		return nil
	}, "env")
	def(func(call *stepCall, v starlark.Value) error {
		r, err := runOf(call, "exports")
		if err != nil {
			return err
		}
		r.exports, err = extractArtifactExports(v, call.variant)
		return err
	}, "exports")
	def(func(call *stepCall, v starlark.Value) error {
		r, err := runOf(call, "expect_exit")
		if err != nil {
			return err
		}
		code, ok := v.(starlark.Int)
		if !ok {
			return fmt.Errorf("%s: expect_exit must be an int", call.variant)
		}
		n64, _ := code.Int64()
		n := int(n64)
		r.expectExit = &n
		return nil
	}, "expect_exit")
	def(func(call *stepCall, v starlark.Value) error {
		r, err := runOf(call, "expect_logs")
		if err != nil {
			return err
		}
		r.expectLogs, err = extractStringOrList(v, call.variant, "expect_logs")
		return err
	}, "expect_logs")
	def(func(call *stepCall, v starlark.Value) error {
		r, err := runOf(call, "fail_on_logs")
		if err != nil {
			return err
		}
		r.failOnLogs, err = extractStringOrList(v, call.variant, "fail_on_logs")
		return err
	}, "fail_on_logs")

	// security.*
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "technique")
		if err != nil {
			return err
		}
		str, err := stringArg(call, "technique", v)
		if err != nil {
			return err
		}
		s.technique = strings.ToLower(str)
		if s.technique != "" && !containsString(model.SecurityTechniques, s.technique) {
			return fmt.Errorf("%s: technique %q is not one of %s", call.variant, str, strings.Join(model.SecurityTechniques, ", "))
		}
		return nil
	}, "technique")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "path")
		if err != nil {
			return err
		}
		s.floodPath, err = stringArg(call, "path", v)
		return err
	}, "path")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "rate")
		if err != nil {
			return err
		}
		if f, ok := starlark.AsFloat(v); ok {
			if f <= 0 || f > model.MaxSecurityFloodRate {
				return fmt.Errorf("%s: rate must be above 0 and at most %g requests per second, got %g", call.variant, model.MaxSecurityFloodRate, f)
			}
			s.floodRate = f
		}
		return nil
	}, "rate")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "duration")
		if err != nil {
			return err
		}
		if f, ok := starlark.AsFloat(v); ok {
			if f <= 0 || f > model.MaxSecurityFloodDuration {
				return fmt.Errorf("%s: duration must be above 0 and at most %g seconds, got %g", call.variant, model.MaxSecurityFloodDuration, f)
			}
			s.floodDuration = f
		}
		return nil
	}, "duration")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "expect_throttle")
		if err != nil {
			return err
		}
		s.floodThrottle, err = boolArg(call, "expect_throttle", v)
		return err
	}, "expect_throttle")
	def(func(call *stepCall, v starlark.Value) error {
		s, err := specAs[*securityArgs](call, "fail_on")
		if err != nil {
			return err
		}
		str, err := stringArg(call, "fail_on", v)
		if err != nil {
			return err
		}
		level := strings.ToLower(str)
		if !containsString(model.SecurityFailLevels, level) {
			return fmt.Errorf("%s: fail_on %q is not one of %s", call.variant, str, strings.Join(model.SecurityFailLevels, ", "))
		}
		s.failOn = level
		return nil
	}, "fail_on")
}

func cloneStringMap(input map[string]string) map[string]string {
	if input == nil {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		output[key] = value
	}
	return output
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

// KeywordTable is the markdown table of the keywords each call of suite.star
// accepts, written from the declarations the evaluator checks calls against.
// Calls that accept the same keywords share a row.
func KeywordTable() string {
	type row struct {
		calls    []string
		keywords string
	}
	var rows []*row
	byKeywords := map[string]*row{}
	variants := make([]string, 0, len(stepKinds))
	for variant := range stepKinds {
		variants = append(variants, variant)
	}
	sort.Strings(variants)
	for _, variant := range variants {
		list := stepKinds[variant].keyList()
		r, ok := byKeywords[list]
		if !ok {
			r = &row{keywords: list}
			byKeywords[list] = r
			rows = append(rows, r)
		}
		r.calls = append(r.calls, "`"+variant+"`")
	}
	var out strings.Builder
	out.WriteString("| Call | Keywords it accepts |\n|------|---------------------|\n")
	for _, r := range rows {
		calls := strings.Join(r.calls, ", ")
		out.WriteString("| " + calls + " | " + backtick(r.keywords) + " |\n")
	}
	return out.String()
}

func backtick(list string) string {
	parts := strings.Split(list, ", ")
	for i, p := range parts {
		if p != "" {
			parts[i] = "`" + p + "`"
		}
	}
	return strings.Join(parts, ", ")
}
