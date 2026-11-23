package runner

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// sidecarListenAddress is where the suite's sidecar answers from inside its own
// container; mock surfaces are routed there by Host header.
const sidecarListenAddress = "127.0.0.1:9080"

// gatewayHostPort is the host:port of a gateway URL, which is the address a gRPC
// client dials: it has no scheme.
func gatewayHostPort(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(raw), "http://"), "https://")
	}
	return parsed.Host
}

// hostAliasFromContainer reaches the machine running the control plane from
// inside the sidecar's container.
const hostAliasFromContainer = "host.docker.internal"

// resolvedTarget is where the load generator should send its requests.
type resolvedTarget struct {
	// URL is the base URL the generator dials, with no trailing slash.
	URL string
	// HostHeader, when set, is sent as the Host of every request. A mock
	// surface is selected by it.
	HostHeader string
	// Note says how the target in suite.star was mapped, for the step log.
	Note string
}

// resolveTrafficTarget turns the target of a traffic step into an address the
// sidecar's load generator can really reach.
func resolveTrafficTarget(ctx context.Context, step StepSpec) (resolvedTarget, error) {
	if step.Load == nil {
		return resolvedTarget{}, fmt.Errorf("traffic step %q has no load spec", step.Node.Name)
	}
	return resolveSidecarTarget(ctx, step, "traffic", "load against a service", step.Load.Target)
}

// resolveSidecarTarget turns the target a suite wrote into an address the
// sidecar can really reach. Traffic and security steps both run inside the
// sidecar, so both resolve their targets here; kind and activity only word the
// errors ("traffic", "load against a service").
//
// The generator runs inside the sidecar's container, not on the control plane
// host and not on the run's network, so a name that is meaningful to the suite
// ("api", "payment-suite.mock.internal", "localhost") means nothing to it as it
// stands:
//
//   - a mock host is served by the sidecar itself, so requests go to its own
//     listener with the mock host as the Host header;
//   - localhost and 127.0.0.1 would be the sidecar itself, so they become the
//     host;
//   - a service of the suite is reached at its container's address on the run's
//     network, after the sidecar has joined that network;
//   - a fully-qualified host or an address is used as written.
//
// A bare name that is none of the above is an error: it used to be given
// made-up traffic, which let a typo or a removed service pass every run.
func resolveSidecarTarget(ctx context.Context, step StepSpec, kind, activity, raw string) (resolvedTarget, error) {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" {
		return resolvedTarget{}, fmt.Errorf("%s step %q: target %q is not a URL", kind, step.Node.Name, raw)
	}
	scheme := parsed.Scheme
	if scheme == "" {
		scheme = "http"
	}
	host := parsed.Hostname()
	path := strings.TrimRight(parsed.EscapedPath(), "/")

	for _, mockHost := range step.MockHosts {
		if strings.EqualFold(mockHost, host) {
			return resolvedTarget{
				URL:        "http://" + sidecarListenAddress + path,
				HostHeader: host,
				Note:       fmt.Sprintf("%s is a mock host of this suite; the sidecar serves it on %s", host, sidecarListenAddress),
			}, nil
		}
	}

	if strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1" {
		return resolvedTarget{
			URL:  scheme + "://" + net.JoinHostPort(hostAliasFromContainer, portOr(parsed, scheme)) + path,
			Note: fmt.Sprintf("%s inside the sidecar is the sidecar itself; using %s", host, hostAliasFromContainer),
		}, nil
	}

	if nodeID, isService := step.ServiceHosts[host]; isService {
		if step.BackendKind == "kubernetes" {
			return resolvedTarget{}, fmt.Errorf("%s step %q: target %q is a service of the suite, and %s is supported only on the local Docker backend, not on %s", kind, step.Node.Name, host, activity, step.BackendKind)
		}
		if err := AttachSidecarToExecution(ctx, step.SuiteID, step.Profile, step.ExecutionID); err != nil {
			return resolvedTarget{}, fmt.Errorf("%s step %q: %w", kind, step.Node.Name, err)
		}
		ip, err := ContainerIPOnExecutionNetwork(ctx, step.ExecutionID, nodeID)
		if err != nil {
			return resolvedTarget{}, fmt.Errorf("%s step %q: service %q is not reachable: %w", kind, step.Node.Name, host, err)
		}
		return resolvedTarget{
			URL:  scheme + "://" + net.JoinHostPort(ip, portOr(parsed, scheme)) + path,
			Note: fmt.Sprintf("%s is the service %q; the sidecar joined the run's network and dials its address %s", host, nodeID, ip),
		}, nil
	}

	if !strings.Contains(host, ".") && net.ParseIP(host) == nil {
		return resolvedTarget{}, fmt.Errorf("%s step %q: target host %q is not a service of this suite, one of its mock hosts, or a fully-qualified host", kind, step.Node.Name, host)
	}
	return resolvedTarget{URL: scheme + "://" + parsed.Host + path, Note: fmt.Sprintf("%s is used as written", host)}, nil
}

// portOr is the target's port, or the default of its scheme.
func portOr(parsed *url.URL, scheme string) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}
