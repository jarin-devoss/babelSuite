package plan

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites/model"
	"github.com/babelsuite/babelsuite/internal/suites/starkit"
	"go.starlark.net/starlark"
)

// A traffic plan is a Starlark program, run by the same interpreter as suite.star:
// loops, variables, functions and env.get() work in it. Its builders (traffic.user,
// traffic.task, traffic.get, ...) return typed values, and traffic.plan(...) names
// the ones that make up the plan. Each builder accepts a fixed set of keywords, so
// a misspelled one is an error naming it instead of being dropped.

// planValue is what a plan builder returns.
type planValue struct {
	typ string
	v   any
}

func (p *planValue) String() string        { return p.typ }
func (p *planValue) Type() string          { return "babelsuite." + p.typ }
func (p *planValue) Freeze()               {}
func (p *planValue) Truth() starlark.Bool  { return starlark.True }
func (p *planValue) Hash() (uint32, error) { return 0, fmt.Errorf("%s is not hashable", p.typ) }

type planUser struct {
	name   string
	weight int
	wait   model.LoadWait
	tasks  []*planValue
}

type planTask struct {
	name    string
	weight  int
	request model.LoadRequest
	checks  []model.LoadThreshold
}

type planDecl struct {
	users      []*planValue
	stages     []model.LoadStage
	thresholds []model.LoadThreshold
}

type planBuild struct {
	decl  *planDecl
	count int
}

// planParam is one parameter of a builder. Positional parameters come first, in
// the order they are listed.
type planParam struct {
	name       string
	positional bool
	aliases    []string
}

// unpackPlan reads a builder call into a map by parameter name. An unknown
// keyword, or a value given twice, is an error that says what the call accepts.
func unpackPlan(call string, params []planParam, args starlark.Tuple, kwargs []starlark.Tuple) (map[string]starlark.Value, error) {
	out := map[string]starlark.Value{}
	var positional []planParam
	known := map[string]string{}
	var names []string
	for _, p := range params {
		known[p.name] = p.name
		names = append(names, p.name)
		for _, alias := range p.aliases {
			known[alias] = p.name
		}
		if p.positional {
			positional = append(positional, p)
		}
	}
	sort.Strings(names)
	if len(args) > len(positional) {
		return nil, fmt.Errorf("%s: takes at most %d positional arguments, got %d", call, len(positional), len(args))
	}
	for i, v := range args {
		out[positional[i].name] = v
	}
	for _, kw := range kwargs {
		key := string(kw[0].(starlark.String))
		name, ok := known[key]
		if !ok {
			return nil, fmt.Errorf("%s: unknown argument %q; allowed: %s", call, key, strings.Join(names, ", "))
		}
		if _, dup := out[name]; dup {
			return nil, fmt.Errorf("%s: %s given more than once", call, name)
		}
		out[name] = kw[1]
	}
	return out, nil
}

func planString(call, name string, v starlark.Value) (string, error) {
	if v == nil || v == starlark.None {
		return "", nil
	}
	s, ok := starlark.AsString(v)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", call, name)
	}
	return strings.TrimSpace(s), nil
}

func planNumber(call, name string, v starlark.Value) (float64, error) {
	if v == nil || v == starlark.None {
		return 0, nil
	}
	f, ok := starlark.AsFloat(v)
	if !ok {
		return 0, fmt.Errorf("%s: %s must be a number", call, name)
	}
	return f, nil
}

func planList(call, name, typ string, v starlark.Value) ([]*planValue, error) {
	if v == nil || v == starlark.None {
		return nil, nil
	}
	seq, ok := v.(starlark.Indexable)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a list of %s values", call, name, typ)
	}
	out := make([]*planValue, 0, seq.Len())
	for i := 0; i < seq.Len(); i++ {
		item, ok := seq.Index(i).(*planValue)
		if !ok || item.typ != typ {
			return nil, fmt.Errorf("%s: %s must be a list of %s values, got %s", call, name, typ, seq.Index(i).Type())
		}
		out = append(out, item)
	}
	return out, nil
}

func thresholds(call, name string, v starlark.Value) ([]model.LoadThreshold, error) {
	items, err := planList(call, name, "threshold", v)
	if err != nil {
		return nil, err
	}
	out := make([]model.LoadThreshold, 0, len(items))
	for _, item := range items {
		out = append(out, item.v.(model.LoadThreshold))
	}
	return out, nil
}

func buildPlanNamespace(build *planBuild) *starkit.Namespace {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("invalid suite topology: "+format, args...)
	}
	m := map[string]starkit.Builder{}

	request := func(method string) starkit.Builder {
		call := "traffic." + strings.ToLower(method)
		return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			a, err := unpackPlan(call, []planParam{{name: "path", positional: true}, {name: "name"}, {name: "headers"}, {name: "json"}, {name: "checks"}}, args, kwargs)
			if err != nil {
				return nil, err
			}
			path, err := planString(call, "path", a["path"])
			if err != nil {
				return nil, err
			}
			if path == "" {
				return nil, fail("%s requires a request path", call)
			}
			name, err := planString(call, "name", a["name"])
			if err != nil {
				return nil, err
			}
			req := model.LoadRequest{Method: method, Path: path, Name: strutil.FirstNonEmpty(name, path)}
			if h := a["headers"]; h != nil && h != starlark.None {
				d, ok := h.(*starlark.Dict)
				if !ok {
					return nil, fmt.Errorf("%s: headers must be a dict", call)
				}
				req.Headers = map[string]string{}
				for _, kv := range d.Items() {
					key, _ := starlark.AsString(kv[0])
					if s, ok := starlark.AsString(kv[1]); ok {
						req.Headers[key] = s
					} else {
						req.Headers[key] = fmt.Sprint(planJSON(kv[1]))
					}
				}
			}
			if body := a["json"]; body != nil && body != starlark.None {
				encoded, err := json.Marshal(planJSON(body))
				if err != nil {
					return nil, fmt.Errorf("%s: json: %w", call, err)
				}
				req.Body = string(encoded)
				if req.Headers == nil {
					req.Headers = map[string]string{}
				}
				if req.Headers["Content-Type"] == "" {
					req.Headers["Content-Type"] = "application/json"
				}
			}
			if req.Checks, err = thresholds(call, "checks", a["checks"]); err != nil {
				return nil, err
			}
			if len(req.Checks) == 0 {
				req.Checks = nil
			}
			return &planValue{typ: "request", v: req}, nil
		}
	}
	m["get"] = request("GET")
	m["post"] = request("POST")

	wait := func(call string, params []planParam, make func(a map[string]starlark.Value) (model.LoadWait, error)) starkit.Builder {
		return func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
			a, err := unpackPlan(call, params, args, kwargs)
			if err != nil {
				return nil, err
			}
			w, err := make(a)
			if err != nil {
				return nil, err
			}
			return &planValue{typ: "wait", v: w}, nil
		}
	}
	required := func(call, name string, a map[string]starlark.Value) (float64, error) {
		if a[name] == nil {
			return 0, fail("%s requires %s", call, name)
		}
		return planNumber(call, name, a[name])
	}
	m["constant"] = wait("traffic.constant", []planParam{{name: "seconds", positional: true}}, func(a map[string]starlark.Value) (model.LoadWait, error) {
		s, err := required("traffic.constant", "seconds", a)
		return model.LoadWait{Mode: "constant", Seconds: s}, err
	})
	m["between"] = wait("traffic.between", []planParam{{name: "min_seconds", positional: true}, {name: "max_seconds", positional: true}}, func(a map[string]starlark.Value) (model.LoadWait, error) {
		lo, err := required("traffic.between", "min_seconds", a)
		if err != nil {
			return model.LoadWait{}, err
		}
		hi, err := required("traffic.between", "max_seconds", a)
		return model.LoadWait{Mode: "between", MinSeconds: lo, MaxSeconds: hi}, err
	})
	m["pacing"] = wait("traffic.pacing", []planParam{{name: "seconds_per_iteration", positional: true}}, func(a map[string]starlark.Value) (model.LoadWait, error) {
		s, err := required("traffic.pacing", "seconds_per_iteration", a)
		return model.LoadWait{Mode: "pacing", Seconds: s}, err
	})

	m["threshold"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		const call = "traffic.threshold"
		a, err := unpackPlan(call, []planParam{{name: "metric", positional: true}, {name: "op", positional: true}, {name: "value", positional: true}, {name: "sampler"}}, args, kwargs)
		if err != nil {
			return nil, err
		}
		metric, err := planString(call, "metric", a["metric"])
		if err != nil {
			return nil, err
		}
		op, err := planString(call, "op", a["op"])
		if err != nil {
			return nil, err
		}
		if metric == "" || op == "" || a["value"] == nil {
			return nil, fail("traffic.threshold requires metric, operator, and value")
		}
		value, err := planNumber(call, "value", a["value"])
		if err != nil {
			return nil, err
		}
		sampler, err := planString(call, "sampler", a["sampler"])
		if err != nil {
			return nil, err
		}
		return &planValue{typ: "threshold", v: model.LoadThreshold{Metric: metric, Op: op, Value: value, Sampler: sampler}}, nil
	}

	m["stage"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		stage, err := buildLoadStage(args, kwargs)
		if err != nil {
			return nil, err
		}
		return &planValue{typ: "stage", v: stage}, nil
	}
	m["stages"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		a, err := unpackPlan("traffic.stages", []planParam{{name: "stages", positional: true}}, args, kwargs)
		if err != nil {
			return nil, err
		}
		items, err := planList("traffic.stages", "stages", "stage", a["stages"])
		if err != nil {
			return nil, err
		}
		stages := make([]model.LoadStage, 0, len(items))
		for _, item := range items {
			stages = append(stages, item.v.(model.LoadStage))
		}
		return &planValue{typ: "stages", v: stages}, nil
	}

	m["task"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		const call = "traffic.task"
		a, err := unpackPlan(call, []planParam{{name: "name", aliases: []string{"id"}}, {name: "request"}, {name: "weight"}, {name: "checks"}}, args, kwargs)
		if err != nil {
			return nil, err
		}
		req, ok := a["request"].(*planValue)
		if !ok || req.typ != "request" {
			return nil, fail("traffic.task must declare request")
		}
		name, err := planString(call, "name", a["name"])
		if err != nil {
			return nil, err
		}
		weight, err := planNumber(call, "weight", a["weight"])
		if err != nil {
			return nil, err
		}
		task := &planTask{name: name, weight: int(weight), request: req.v.(model.LoadRequest)}
		if task.weight <= 0 {
			task.weight = 1
		}
		checks, err := thresholds(call, "checks", a["checks"])
		if err != nil {
			return nil, err
		}
		task.checks = append(task.checks, checks...)
		task.checks = append(task.checks, task.request.Checks...)
		return &planValue{typ: "task", v: task}, nil
	}

	m["user"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		const call = "traffic.user"
		a, err := unpackPlan(call, []planParam{{name: "name", aliases: []string{"id"}}, {name: "weight"}, {name: "wait"}, {name: "tasks"}}, args, kwargs)
		if err != nil {
			return nil, err
		}
		tasks, err := planList(call, "tasks", "task", a["tasks"])
		if err != nil {
			return nil, err
		}
		if a["tasks"] == nil || a["tasks"] == starlark.None {
			return nil, fail("traffic.user must declare tasks")
		}
		name, err := planString(call, "name", a["name"])
		if err != nil {
			return nil, err
		}
		weight, err := planNumber(call, "weight", a["weight"])
		if err != nil {
			return nil, err
		}
		user := &planUser{name: name, weight: int(weight), tasks: tasks}
		if user.weight <= 0 {
			user.weight = 1
		}
		if w, ok := a["wait"].(*planValue); ok && w.typ == "wait" {
			user.wait = w.v.(model.LoadWait)
		} else if a["wait"] != nil && a["wait"] != starlark.None {
			return nil, fmt.Errorf("%s: wait must be traffic.constant, traffic.between or traffic.pacing", call)
		}
		return &planValue{typ: "user", v: user}, nil
	}

	m["plan"] = func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		const call = "traffic.plan"
		if build.count > 0 {
			return nil, fail("traffic plan declares traffic.plan more than once")
		}
		build.count++
		a, err := unpackPlan(call, []planParam{{name: "users"}, {name: "shape"}, {name: "thresholds"}}, args, kwargs)
		if err != nil {
			return nil, err
		}
		if a["users"] == nil || a["users"] == starlark.None {
			return nil, fail("traffic.plan must declare users")
		}
		users, err := planList(call, "users", "user", a["users"])
		if err != nil {
			return nil, err
		}
		build.decl.users = users
		if shape := a["shape"]; shape != nil && shape != starlark.None {
			stages, ok := shape.(*planValue)
			if !ok || stages.typ != "stages" {
				return nil, fail("traffic.plan shape must use traffic.stages(...)")
			}
			build.decl.stages = stages.v.([]model.LoadStage)
		}
		if build.decl.thresholds, err = thresholds(call, "thresholds", a["thresholds"]); err != nil {
			return nil, err
		}
		return starlark.None, nil
	}

	return &starkit.Namespace{Methods: m}
}

// buildLoadStage reads traffic.stage(duration, users=, spawn_rate=, stop=).
func buildLoadStage(args starlark.Tuple, kwargs []starlark.Tuple) (model.LoadStage, error) {
	const call = "traffic.stage"
	a, err := unpackPlan(call, []planParam{{name: "duration", positional: true}, {name: "users"}, {name: "spawn_rate"}, {name: "stop"}}, args, kwargs)
	if err != nil {
		return model.LoadStage{}, err
	}
	durationValue, err := planString(call, "duration", a["duration"])
	if err != nil {
		return model.LoadStage{}, err
	}
	if durationValue == "" {
		return model.LoadStage{}, fmt.Errorf("invalid suite topology: traffic.stage requires duration")
	}
	duration, err := time.ParseDuration(durationValue)
	if err != nil {
		return model.LoadStage{}, fmt.Errorf("invalid suite topology: invalid traffic stage duration %q", durationValue)
	}
	if duration <= 0 {
		return model.LoadStage{}, fmt.Errorf("invalid suite topology: traffic stage duration must be positive, got %q", durationValue)
	}
	users, err := planNumber(call, "users", a["users"])
	if err != nil {
		return model.LoadStage{}, err
	}
	spawn, err := planNumber(call, "spawn_rate", a["spawn_rate"])
	if err != nil {
		return model.LoadStage{}, err
	}
	stop := false
	if v, ok := a["stop"].(starlark.Bool); ok {
		stop = bool(v)
	} else if a["stop"] != nil && a["stop"] != starlark.None {
		return model.LoadStage{}, fmt.Errorf("%s: stop must be a bool", call)
	}
	return model.LoadStage{Duration: duration, Users: int(users), SpawnRate: spawn, Stop: stop}, nil
}

// Parse runs a traffic plan with the launch profile's env and returns the
// spec it declares, on top of base (the traffic step's own settings).
func Parse(base *model.LoadSpec, source string, env map[string]string) (*model.LoadSpec, error) {
	spec := model.CloneLoadSpec(base)
	if spec == nil {
		spec = &model.LoadSpec{}
	}

	build := &planBuild{decl: &planDecl{}}
	namespace := buildPlanNamespace(build)
	thread := &starlark.Thread{
		Name: "traffic-plan",
		// A plan can load the runtime module, as suite.star does, for traffic.
		Load: func(_ *starlark.Thread, module string) (starlark.StringDict, error) {
			if module == "@babelsuite/runtime" {
				return starlark.StringDict{"traffic": namespace}, nil
			}
			return nil, fmt.Errorf("load(%q): a traffic plan can load only @babelsuite/runtime", module)
		},
	}
	thread.SetMaxExecutionSteps(starkit.MaxSteps)
	globals, err := starlark.ExecFileOptions(starkit.FileOptions, thread, "traffic-plan.star", source, starlark.StringDict{
		"traffic": namespace,
		"env":     starkit.FrozenEnv(env),
	})
	if err != nil {
		message := describeStarlarkError(err)
		if !strings.HasPrefix(message, "invalid suite topology:") {
			message = "invalid suite topology: " + message
		}
		return nil, fmt.Errorf("%s", message)
	}
	if build.count == 0 {
		return nil, fmt.Errorf("invalid suite topology: traffic plan %q must declare traffic.plan(...)", spec.PlanPath)
	}

	nameOf := planNames(globals)
	spec.Users = make([]model.LoadUser, 0, len(build.decl.users))
	for _, item := range build.decl.users {
		user := item.v.(*planUser)
		id := nameOf[item]
		loaded := model.LoadUser{ID: id, Name: strutil.FirstNonEmpty(user.name, id), Weight: user.weight, Wait: user.wait}
		for _, taskItem := range user.tasks {
			task := taskItem.v.(*planTask)
			taskID := nameOf[taskItem]
			loaded.Tasks = append(loaded.Tasks, model.LoadTask{
				ID:      taskID,
				Name:    strutil.FirstNonEmpty(task.name, task.request.Name, taskID),
				Weight:  task.weight,
				Request: task.request,
				Checks:  task.checks,
			})
		}
		spec.Users = append(spec.Users, loaded)
	}
	spec.Stages = build.decl.stages
	if len(build.decl.thresholds) > 0 {
		spec.Thresholds = build.decl.thresholds
	}

	if err := Validate(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

// planNames maps each plan value to the variable it was assigned to, the way a
// suite.star step is named after its variable. When a value has several names the
// longest wins, then the first alphabetically.
func planNames(globals starlark.StringDict) map[*planValue]string {
	names := map[*planValue]string{}
	for name, v := range globals {
		item, ok := v.(*planValue)
		if !ok {
			continue
		}
		if existing, seen := names[item]; !seen || len(name) > len(existing) || (len(name) == len(existing) && name < existing) {
			names[item] = name
		}
	}
	return names
}

func describeStarlarkError(err error) string {
	if evalErr, ok := err.(*starlark.EvalError); ok {
		return evalErr.Msg
	}
	return err.Error()
}

// planJSON turns a plan value into what encoding/json writes: dicts become
// objects, lists arrays, and keys are written sorted.
func planJSON(v starlark.Value) any {
	switch x := v.(type) {
	case starlark.NoneType:
		return nil
	case starlark.Bool:
		return bool(x)
	case starlark.String:
		return string(x)
	case starlark.Int:
		if i, ok := x.Int64(); ok {
			return i
		}
		return x.String()
	case starlark.Float:
		return float64(x)
	case *starlark.List:
		out := make([]any, 0, x.Len())
		for i := 0; i < x.Len(); i++ {
			out = append(out, planJSON(x.Index(i)))
		}
		return out
	case starlark.Tuple:
		out := make([]any, 0, len(x))
		for _, item := range x {
			out = append(out, planJSON(item))
		}
		return out
	case *starlark.Dict:
		out := make(map[string]any, x.Len())
		for _, kv := range x.Items() {
			key, ok := starlark.AsString(kv[0])
			if !ok {
				key = kv[0].String()
			}
			out[key] = planJSON(kv[1])
		}
		return out
	default:
		return v.String()
	}
}
