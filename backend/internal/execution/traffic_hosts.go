package execution

import (
	"net/url"
	"strings"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// trafficServiceHosts maps every hostname a service of this execution answers
// to onto the id of the node that provides it, for a traffic step to turn a
// target such as http://api:8080 into the address of that node's container.
// Other steps have no use for it.
func (s *Service) trafficServiceHosts(executionID string, suite *suites.Definition, node topologyNode) map[string]string {
	if node.Kind != suites.NodeKindTraffic || suite == nil {
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

// trafficMockHosts lists the hostnames the suite's mock surfaces answer to
// (payment-suite.mock.internal and the like). The sidecar serves them itself, so
// a traffic step that targets one is sent to the sidecar's own listener.
func trafficMockHosts(suite *suites.Definition, node topologyNode) []string {
	if node.Kind != suites.NodeKindTraffic || suite == nil {
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
