package suites

import (
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"
)

// defaultLoadRate is the arrival rate of an open-model run that names no rate.
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
func applyDefaultShape(spec *LoadSpec) {
	if spec == nil || len(spec.Stages) > 0 || len(spec.Users) > 0 {
		return
	}

	step := func(users int, seconds int) LoadStage {
		return LoadStage{Users: users, SpawnRate: float64(users), Duration: time.Duration(seconds) * time.Second}
	}

	switch spec.Variant {
	case "traffic.smoke":
		spec.Stages = []LoadStage{step(1, 10)}
	case "traffic.stress", "traffic.staged":
		spec.Stages = []LoadStage{step(5, 20), step(20, 20), step(32, 20)}
	case "traffic.spike":
		spec.Stages = []LoadStage{step(5, 15), step(32, 15), step(5, 15)}
	case "traffic.soak":
		// Endurance by default means a minute, not the five-minute cap, so an
		// example suite does not hold a run for that long. A plan can ask for more.
		spec.Stages = []LoadStage{step(5, 60)}
	case "traffic.step":
		spec.Stages = []LoadStage{step(4, 15), step(8, 15), step(12, 15), step(16, 15)}
	case "traffic.wave":
		spec.Stages = []LoadStage{step(4, 15), step(16, 15), step(4, 15), step(16, 15)}
	case "traffic.scalability":
		spec.Stages = []LoadStage{step(1, 10), step(2, 10), step(4, 10), step(8, 10), step(16, 10), step(32, 10)}
	case "traffic.constant_throughput", "traffic.open_model":
		spec.Stages = []LoadStage{{Duration: 30 * time.Second}}
		if spec.RequestsPerS <= 0 && spec.ArrivalRate <= 0 {
			spec.RequestsPerS = defaultLoadRate
		}
	case "traffic.constant_pacing":
		spec.Stages = []LoadStage{step(5, 30)}
	default: // traffic.baseline and anything unnamed
		spec.Stages = []LoadStage{step(5, 30)}
	}

	wait := LoadWait{Mode: "constant", Seconds: 0}
	if spec.Variant == "traffic.constant_pacing" {
		wait = LoadWait{Mode: "pacing", Seconds: 1}
	}
	spec.Users = []LoadUser{{
		ID:     "default",
		Name:   "default",
		Weight: 1,
		Wait:   wait,
		Tasks: []LoadTask{{
			ID:      "get-root",
			Name:    "GET /",
			Weight:  1,
			Request: LoadRequest{Method: "GET", Path: "/", Name: "GET /"},
		}},
	}}
}

// trafficTargetHost returns the host a traffic target dials, or "" when the
// target does not parse.
func trafficTargetHost(target string) string {
	parsed, err := url.Parse(strings.TrimSpace(target))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(parsed.Hostname())
}

// isSuiteLocalName reports whether a host is a bare name: the kind that only
// means something inside the suite's own network, because it has no dot, is not
// an address and is not the machine itself.
func isSuiteLocalName(host string) bool {
	if host == "" || strings.Contains(host, ".") || strings.EqualFold(host, "localhost") {
		return false
	}
	return net.ParseIP(host) == nil
}

// validateTrafficTargets rejects a traffic target that names a host nothing
// provides. A bare name resolves only to one of the suite's own services, so a
// name that is none of them (a typo, a service that was removed, or a name from
// the days when such a target was quietly given made-up traffic) used to pass
// every run. It is now an error when the suite is loaded, where an author sees
// it, rather than a failure in the middle of a run.
//
// A name counts as the suite's own when it is the name of any node of the
// resolved topology, with or without the suite prefix a composed suite's
// contested name carries.
func validateTrafficTargets(suite Definition, nodes []TopologyNode) error {
	known := make(map[string]struct{}, len(nodes)*2)
	for _, node := range nodes {
		name := strings.TrimSpace(node.Name)
		if name == "" {
			continue
		}
		alias := name
		if index := strings.LastIndex(name, "/"); index >= 0 {
			alias = name[index+1:]
		}
		known[name] = struct{}{}
		known[alias] = struct{}{}
		if owner := strings.TrimSpace(node.SourceSuiteID); owner != "" {
			known[owner+"-"+alias] = struct{}{}
		}
	}

	for _, node := range nodes {
		if node.Kind != NodeKindTraffic || node.Load == nil || node.SourceSuiteID != suite.ID {
			continue
		}
		host := trafficTargetHost(node.Load.Target)
		if !isSuiteLocalName(host) {
			continue
		}
		if _, found := known[host]; !found {
			return fmt.Errorf("invalid suite topology: traffic step %q targets host %q, which is not a service in this suite; use a service name from the suite, the suite's mock host, or a fully-qualified host", node.Name, host)
		}
	}
	return nil
}
