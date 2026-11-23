package execution

import (
	"net/url"
	"strings"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// runsInSidecar reports whether a step generates its requests from inside the
// suite's sidecar, so its target has to be turned into an address the sidecar
// can reach: a traffic step and a security step.
func runsInSidecar(node topologyNode) bool {
	return node.Kind == suites.NodeKindTraffic || node.Kind == suites.NodeKindSecurity
}

// sidecarServiceHosts maps every hostname a service of this execution answers
// to onto the id of the node that provides it, for a traffic or security step
// to turn a target such as http://api:8080 into the address of that node's
// container. Other steps have no use for it.
func (s *Service) sidecarServiceHosts(executionID string, suite *suites.Definition, node topologyNode) map[string]string {
	if !runsInSidecar(node) || suite == nil {
		return nil
	}
	plan := s.networkPlan(executionID)
	hosts := make(map[string]string, len(suite.Topology))
	for _, other := range suite.Topology {
		for _, alias := range plan.aliasesForNode(other, suite.ID) {
			hosts[alias] = other.ID
		}
	}
	return hosts
}

// sidecarMockHosts lists the hostnames the suite's mock surfaces answer to
// (payment-suite.mock.internal and the like). The sidecar serves them itself, so
// a traffic or security step that targets one is sent to the sidecar's own
// listener.
func sidecarMockHosts(suite *suites.Definition, node topologyNode) []string {
	if !runsInSidecar(node) {
		return nil
	}
	return suiteMockHosts(suite)
}

// suiteMockHosts lists the hostnames the suite's mock surfaces answer to.
func suiteMockHosts(suite *suites.Definition) []string {
	if suite == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(suite.APISurfaces))
	var hosts []string
	for _, surface := range suite.APISurfaces {
		host := ""
		if parsed, err := url.Parse(strings.TrimSpace(surface.MockHost)); err == nil {
			host = strings.ToLower(parsed.Hostname())
		}
		if host == "" {
			// A surface with no declared host (a SOAP contract, say) is routed
			// by path alone, so it answers on any host. The suite's conventional
			// mock host stands for it.
			if strings.TrimSpace(suite.ID) == "" {
				continue
			}
			host = strings.ToLower(strings.TrimSpace(suite.ID)) + ".mock.internal"
		}
		if _, dup := seen[host]; dup {
			continue
		}
		seen[host] = struct{}{}
		hosts = append(hosts, host)
	}
	return hosts
}

// suiteHardened reports whether a mock of the suite opted into gateway-level
// defences, for a security step that scans the mock to say what it measures.
func suiteHardened(suite *suites.Definition, node topologyNode) bool {
	if node.Kind != suites.NodeKindSecurity || suite == nil {
		return false
	}
	for _, other := range suite.Topology {
		if other.Kind == suites.NodeKindMock && other.Harden {
			return true
		}
	}
	return false
}

// suiteMockNames lists the names of the suite's mock nodes, which the gateway
// answers to on the run's network: a service reaches a mock as
// http://stripe-mock:9080. A name that a service of the suite already answers to
// is left out, because the service owns it.
func suiteMockNames(suite *suites.Definition) []string {
	if suite == nil {
		return nil
	}
	services := map[string]struct{}{}
	for _, node := range suite.Topology {
		if addressableKind(node.Kind) {
			services[strings.ToLower(networkAlias(node.Name))] = struct{}{}
		}
	}
	seen := map[string]struct{}{}
	var names []string
	for _, node := range suite.Topology {
		if node.Kind != suites.NodeKindMock {
			continue
		}
		name := strings.ToLower(networkAlias(node.Name))
		if name == "" {
			continue
		}
		if _, taken := services[name]; taken {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names
}
