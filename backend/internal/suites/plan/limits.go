package plan

import (
	"fmt"
	"net/url"
	"time"

	"github.com/babelsuite/babelsuite/internal/suites/model"
)

const (
	MaxPlanBytes   = 128 * 1024
	maxLoadUsers       = 32
	maxLoadStages      = 12
	maxLoadTasks       = 24
	maxLoadDuration    = 5 * time.Minute
	maxLoadRequestRate = 100.0
)

const defaultLoadRate = 5

// applyDefaultShape gives a traffic node that names no plan file the workload
// its variant promises. Before this every variant without a plan was the same
// run (one user for a minute), so "stress" and "baseline" differed only by
// name. The shapes here are documented in docs/runtime-library.md; a plan file
// replaces them entirely.
//
// A closed-model variant given rps= runs as an open model at that rate, with
// the same stages' durations (see the runner), because rps= names arrivals per
// second, which a count of virtual users cannot honour.
func ApplyDefaultShape(spec *model.LoadSpec) {
	if spec == nil || len(spec.Stages) > 0 || len(spec.Users) > 0 {
		return
	}

	step := func(users int, seconds int) model.LoadStage {
		return model.LoadStage{Users: users, SpawnRate: float64(users), Duration: time.Duration(seconds) * time.Second}
	}

	switch spec.Variant {
	case "traffic.smoke":
		spec.Stages = []model.LoadStage{step(1, 10)}
	case "traffic.stress", "traffic.staged":
		spec.Stages = []model.LoadStage{step(5, 20), step(20, 20), step(32, 20)}
	case "traffic.spike":
		spec.Stages = []model.LoadStage{step(5, 15), step(32, 15), step(5, 15)}
	case "traffic.soak":
		// Endurance by default means a minute, not the five-minute cap, so an
		// example suite does not hold a run for that long. A plan can ask for more.
		spec.Stages = []model.LoadStage{step(5, 60)}
	case "traffic.step":
		spec.Stages = []model.LoadStage{step(4, 15), step(8, 15), step(12, 15), step(16, 15)}
	case "traffic.wave":
		spec.Stages = []model.LoadStage{step(4, 15), step(16, 15), step(4, 15), step(16, 15)}
	case "traffic.scalability":
		spec.Stages = []model.LoadStage{step(1, 10), step(2, 10), step(4, 10), step(8, 10), step(16, 10), step(32, 10)}
	case "traffic.constant_throughput", "traffic.open_model":
		spec.Stages = []model.LoadStage{{Duration: 30 * time.Second}}
		if spec.RequestsPerS <= 0 && spec.ArrivalRate <= 0 {
			spec.RequestsPerS = defaultLoadRate
		}
	case "traffic.constant_pacing":
		spec.Stages = []model.LoadStage{step(5, 30)}
	default: // traffic.baseline and anything unnamed
		spec.Stages = []model.LoadStage{step(5, 30)}
	}

	wait := model.LoadWait{Mode: "constant", Seconds: 0}
	if spec.Variant == "traffic.constant_pacing" {
		wait = model.LoadWait{Mode: "pacing", Seconds: 1}
	}
	spec.Users = []model.LoadUser{{
		ID:     "default",
		Name:   "default",
		Weight: 1,
		Wait:   wait,
		Tasks: []model.LoadTask{{
			ID:      "get-root",
			Name:    "GET /",
			Weight:  1,
			Request: model.LoadRequest{Method: "GET", Path: "/", Name: "GET /"},
		}},
	}}
}

// trafficTargetHost returns the host a traffic target dials, or "" when the
// target does not parse.
func Validate(spec *model.LoadSpec) error {
	if spec == nil {
		return fmt.Errorf("invalid suite topology: missing traffic spec")
	}

	parsedTarget, err := url.Parse(spec.Target)
	if err != nil || parsedTarget.Scheme == "" || parsedTarget.Host == "" {
		return fmt.Errorf("invalid suite topology: traffic target %q must be an absolute URL", spec.Target)
	}

	if spec.MinUsers < 0 || spec.MinUsers > maxLoadUsers {
		return fmt.Errorf("invalid suite topology: min_users must be between 0 and %d, got %d", maxLoadUsers, spec.MinUsers)
	}
	if spec.MinUsers > 0 && spec.Variant != "traffic.scalability" {
		return fmt.Errorf("invalid suite topology: min_users applies only to traffic.scalability, not %s", spec.Variant)
	}

	// A task's status check names the status it expects, so only equality can be
	// honoured: the generator is told which statuses are successes.
	for _, user := range spec.Users {
		for _, task := range user.Tasks {
			for _, check := range append(append([]model.LoadThreshold{}, task.Checks...), task.Request.Checks...) {
				if check.Metric == "status" && check.Op != "==" {
					return fmt.Errorf("invalid suite topology: traffic task %q: a status check supports only ==, got %q", task.Name, check.Op)
				}
			}
		}
	}

	// With no plan file the spec holds the variant's default shape (see
	// applyDefaultShape), so only the target and the rates need checking.
	if spec.PlanPath == "" {
		if spec.RequestsPerS > maxLoadRequestRate {
			return fmt.Errorf("invalid suite topology: traffic target %q exceeds the %.0f requests-per-second safety limit", spec.Target, maxLoadRequestRate)
		}
		if spec.ArrivalRate > maxLoadRequestRate {
			return fmt.Errorf("invalid suite topology: traffic target %q exceeds the %.0f arrivals-per-second safety limit", spec.Target, maxLoadRequestRate)
		}
		return nil
	}

	if len(spec.Users) == 0 {
		return fmt.Errorf("invalid suite topology: traffic plan %q must declare at least one user", spec.PlanPath)
	}
	if len(spec.Stages) == 0 {
		return fmt.Errorf("invalid suite topology: traffic plan %q must declare at least one stage", spec.PlanPath)
	}
	if len(spec.Stages) > maxLoadStages {
		return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %d stage safety limit", spec.PlanPath, maxLoadStages)
	}

	totalDuration := time.Duration(0)
	maxUsers := 0
	for _, stage := range spec.Stages {
		if stage.Duration <= 0 {
			return fmt.Errorf("invalid suite topology: traffic plan %q contains a non-positive stage duration", spec.PlanPath)
		}
		totalDuration += stage.Duration
		if stage.Users > maxUsers {
			maxUsers = stage.Users
		}
	}
	if totalDuration > maxLoadDuration {
		return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %s duration safety limit", spec.PlanPath, maxLoadDuration)
	}
	if maxUsers > maxLoadUsers {
		return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %d user safety limit", spec.PlanPath, maxLoadUsers)
	}

	totalTasks := 0
	for userIndex := range spec.Users {
		if spec.Users[userIndex].Weight <= 0 {
			spec.Users[userIndex].Weight = 1
		}
		if len(spec.Users[userIndex].Tasks) == 0 {
			return fmt.Errorf("invalid suite topology: traffic user %q does not declare any tasks", spec.Users[userIndex].Name)
		}
		totalTasks += len(spec.Users[userIndex].Tasks)
		if totalTasks > maxLoadTasks {
			return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %d task safety limit", spec.PlanPath, maxLoadTasks)
		}
		for taskIndex := range spec.Users[userIndex].Tasks {
			if spec.Users[userIndex].Tasks[taskIndex].Weight <= 0 {
				spec.Users[userIndex].Tasks[taskIndex].Weight = 1
			}
			if spec.Users[userIndex].Tasks[taskIndex].Request.Method == "" || spec.Users[userIndex].Tasks[taskIndex].Request.Path == "" {
				return fmt.Errorf("invalid suite topology: traffic task %q must declare a request method and path", spec.Users[userIndex].Tasks[taskIndex].Name)
			}
		}
		if spec.Users[userIndex].Wait.Mode == "" {
			if spec.Variant == "traffic.constant_pacing" {
				spec.Users[userIndex].Wait = model.LoadWait{Mode: "pacing", Seconds: 1}
			} else {
				spec.Users[userIndex].Wait = model.LoadWait{Mode: "constant", Seconds: 0}
			}
		}
	}

	if spec.Variant == "traffic.constant_throughput" && spec.RequestsPerS <= 0 {
		spec.RequestsPerS = float64(max(1, maxUsers))
	}
	if spec.Variant == "traffic.open_model" && spec.ArrivalRate <= 0 {
		spec.ArrivalRate = float64(max(1, maxUsers))
	}
	if spec.RequestsPerS > maxLoadRequestRate {
		return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %.0f requests-per-second safety limit", spec.PlanPath, maxLoadRequestRate)
	}
	if spec.ArrivalRate > maxLoadRequestRate {
		return fmt.Errorf("invalid suite topology: traffic plan %q exceeds the %.0f arrivals-per-second safety limit", spec.PlanPath, maxLoadRequestRate)
	}

	return nil
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
