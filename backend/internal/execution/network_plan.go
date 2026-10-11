package execution

import (
	"sort"
	"strings"

	"github.com/babelsuite/babelsuite/internal/strutil"
	"github.com/babelsuite/babelsuite/internal/suites"
)

// Node names are scoped to the suite that declared them, but one execution
// network is flat: every container joins it and answers to the last segment of
// its node name. Composing suites therefore reintroduces exactly the collisions
// that namespacing the node IDs was meant to prevent — three dependencies that
// each call pg() all contribute a service whose alias is "db", Docker lets them
// share it, and `psql ... @db:5432` lands on whichever one DNS returned.
//
// The fix is to stop publishing the contested name more than once:
//
//   - The suite being run keeps the bare name. It owns the root namespace, so
//     its own steps address their own services exactly as before.
//   - An imported suite whose service name is contested publishes a qualified
//     alias instead — "identity-broker-db" — and does not claim the bare one.
//   - Everything uncontested is untouched, so a dependency's "broker-api" is
//     still reachable as "broker-api".
//
// The result is that a name resolves to one container or to nothing. Nothing
// adds infrastructure: no extra networks, no address lookups, no ordering
// dependency. Networking stays what the profile says it is — see
// docs/suite-authoring.md, "Profile-Controlled Hardware, Memory and Networking".
//
// A dependency is pointed at its own service the documented way, with `inputs:`
// in dependencies.yaml, which the shipped modules and scripts already honour
// because they read "${POSTGRES_URL:-...}" before falling back to a hostname:
//
//	identity-broker:
//	  inputs:
//	    POSTGRES_URL: postgresql://postgres:postgres@identity-broker-db:5432/app
//
// Unwired, the fallback resolves to nothing and the step fails saying so, which
// is strictly better than silently reading another suite's database.
type networkPlan struct {
	rootSuiteID string
	// contested holds the bare service names more than one suite declares.
	contested map[string]struct{}
}

// networkAlias is the hostname a node would answer to, which is the last path
// segment of its node name.
func networkAlias(name string) string {
	trimmed := strings.TrimSpace(name)
	if idx := strings.LastIndex(trimmed, "/"); idx >= 0 {
		return trimmed[idx+1:]
	}
	return trimmed
}

// addressableKind reports whether other steps can reach this node by hostname.
// Only long-running containers answer on the network: a task or test exits, and
// a mock is served by the APISIX sidecar rather than a container of its own.
func addressableKind(kind string) bool {
	return strings.TrimSpace(strings.ToLower(kind)) == suites.NodeKindService
}

func nodeOwner(node topologyNode, suiteID string) string {
	return strutil.FirstNonEmpty(strings.TrimSpace(node.SourceSuiteID), suiteID)
}

// qualifiedAlias is the name an imported suite's contested service answers to.
// It must stay predictable, because a dependency's `inputs:` names it by hand.
func qualifiedAlias(owner, alias string) string {
	return sanitizeAliasSegment(owner) + "-" + alias
}

// sanitizeAliasSegment keeps a suite id usable inside a DNS name.
func sanitizeAliasSegment(value string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

func buildNetworkPlan(nodes []topologyNode, suiteID string) networkPlan {
	plan := networkPlan{rootSuiteID: suiteID, contested: map[string]struct{}{}}

	owners := map[string]map[string]struct{}{}
	for _, node := range nodes {
		if !addressableKind(node.Kind) {
			continue
		}
		alias := networkAlias(node.Name)
		if alias == "" {
			continue
		}
		if owners[alias] == nil {
			owners[alias] = map[string]struct{}{}
		}
		owners[alias][nodeOwner(node, suiteID)] = struct{}{}
	}

	for alias, claimants := range owners {
		// One claimant means the shared network is already unambiguous.
		if len(claimants) > 1 {
			plan.contested[alias] = struct{}{}
		}
	}
	return plan
}

// networkPlan reads an execution's plan under the lock, mirroring how the other
// per-node resolvers reach into execution state.
func (s *Service) networkPlan(executionID string) networkPlan {
	s.mu.Lock()
	item := s.executions[executionID]
	s.mu.Unlock()
	if item == nil {
		return networkPlan{}
	}
	return item.networks
}

// contestedNames lists the service names more than one suite declares, for a
// boot event that tells an author which dependencies need wiring.
func (p networkPlan) contestedNames() []string {
	out := make([]string, 0, len(p.contested))
	for alias := range p.contested {
		out = append(out, alias)
	}
	sort.Strings(out)
	return out
}

// aliasesForNode returns the hostnames this node's container answers to on the
// execution network. Nil means it joins without publishing a name, which is
// correct for every step that nothing dials.
func (p networkPlan) aliasesForNode(node topologyNode, suiteID string) []string {
	if !addressableKind(node.Kind) {
		return nil
	}
	alias := networkAlias(node.Name)
	if alias == "" {
		return nil
	}

	owner := nodeOwner(node, suiteID)
	if _, contested := p.contested[alias]; !contested || owner == p.rootSuiteID {
		return []string{alias}
	}
	return []string{qualifiedAlias(owner, alias)}
}
