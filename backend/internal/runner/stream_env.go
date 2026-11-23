package runner

import (
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
)

// streamMockEnv tells a step where the suite's TCP and UDP mocks answer. A
// request/response transport has nothing on the wire to say which operation it is
// for, so every operation has a port of its own, and the step is given the list:
//
//	BABELSUITE_TCP_MOCKS=<surface>/<operation>=<host>:<port>,...
//	BABELSUITE_UDP_MOCKS=<surface>/<operation>=<host>:<port>,...
//
// A step on the run's network reaches the gateway under its name and the port the
// sidecar listens on. Any other step (a Kubernetes pod, a remote agent's) goes
// through the host the gateway URL names, and the port the sidecar published.
func streamMockEnv(step StepSpec, gatewayURL string, onRunNetwork bool) []string {
	if len(step.StreamMocks) == 0 {
		return nil
	}
	host := GatewayAlias
	if !onRunNetwork {
		parsed, err := url.Parse(strings.TrimSpace(gatewayURL))
		if err != nil || parsed.Hostname() == "" {
			return nil
		}
		host = parsed.Hostname()
	}

	entries := map[string][]string{}
	for _, mock := range step.StreamMocks {
		port := strconv.Itoa(mock.Port)
		if !onRunNetwork {
			published := SuiteSidecarHostPort(step.SuiteID, step.Profile, fmt.Sprintf("%d/%s", mock.Port, mock.Transport))
			if published == "" {
				continue
			}
			port = published
		}
		entries[mock.Transport] = append(entries[mock.Transport],
			fmt.Sprintf("%s/%s=%s", mock.Surface, mock.Operation, net.JoinHostPort(host, port)))
	}

	var env []string
	if list := entries["tcp"]; len(list) > 0 {
		env = append(env, "BABELSUITE_TCP_MOCKS="+strings.Join(list, ","))
	}
	if list := entries["udp"]; len(list) > 0 {
		env = append(env, "BABELSUITE_UDP_MOCKS="+strings.Join(list, ","))
	}
	return env
}
