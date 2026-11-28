package topology

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

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
	known := suiteNodeNames(nodes)

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

// suiteNodeNames is every name a node of the resolved topology answers to, with
// or without the suite prefix a composed suite's contested name carries.
func suiteNodeNames(nodes []TopologyNode) map[string]struct{} {
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
	return known
}

// validateSecurityTargets rejects a security step that would scan nothing:
//
//   - a bare host that is not a service of the suite, the same rule a traffic
//     target follows;
//   - no target in a suite with no mock, because the default target is the
//     suite's own mock surface and there is none to scan.
func validateSecurityTargets(suite Definition, nodes []TopologyNode) error {
	known := suiteNodeNames(nodes)
	hasMock := false
	for _, node := range nodes {
		if node.Kind == NodeKindMock {
			hasMock = true
		}
	}

	for _, node := range nodes {
		if node.Kind != NodeKindSecurity || node.Security == nil || node.SourceSuiteID != suite.ID {
			continue
		}
		target := strings.TrimSpace(node.Security.Target)
		if target == "" {
			if !hasMock {
				return fmt.Errorf("invalid suite topology: security step %q needs target=: the suite has no mock surface to scan by default", node.Name)
			}
			continue
		}
		host := trafficTargetHost(target)
		if !isSuiteLocalName(host) {
			continue
		}
		if _, found := known[host]; !found {
			return fmt.Errorf("invalid suite topology: security step %q targets host %q, which is not a service in this suite; use a service name from the suite, the suite's mock host, or a fully-qualified host", node.Name, host)
		}
	}
	return nil
}
