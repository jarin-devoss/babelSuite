package apisix

import (
	"net/url"
	"strings"

)

func publicPath(operation OperationConfig) string {
	if strings.HasPrefix(strings.TrimSpace(operation.Name), "/") {
		return strings.TrimSpace(operation.Name)
	}
	if raw := strings.TrimSpace(operation.MockURL); raw != "" {
		if parsed, err := url.Parse(raw); err == nil && strings.TrimSpace(parsed.Path) != "" {
			return parsed.Path
		}
	}
	if strings.TrimSpace(operation.Name) != "" {
		return "/" + strings.Trim(strings.TrimSpace(operation.Name), "/")
	}
	return "/" + strings.TrimSpace(operation.ID)
}

func matchURI(operation OperationConfig) string {
	path := publicPath(operation)
	if !hasPathParams(path) {
		return path
	}

	replaced := pathParameterPattern.ReplaceAllString(path, "*")
	if strings.TrimSpace(replaced) == "" {
		return "/*"
	}
	return replaced
}

func httpMethod(operation OperationConfig) string {
	method := strings.ToUpper(strings.TrimSpace(operation.Method))
	if method == "" || method == "RPC" || method == "EVENT" {
		return "POST"
	}
	return method
}

// routeHosts are the hosts a mock route answers to: the surface's own host and the
// names of the suite's mock nodes. A surface with no host of its own is routed by
// path alone, so it answers on any host and needs no list.
func routeHosts(suite SuiteConfig, surface SurfaceConfig) []string {
	host := strings.TrimSpace(hostOnly(surface.MockHost))
	if host == "" {
		return nil
	}
	hosts := []string{host}
	seen := map[string]struct{}{strings.ToLower(host): {}}
	for _, name := range suite.MockNames {
		name = strings.ToLower(strings.TrimSpace(name))
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		hosts = append(hosts, name)
	}
	return hosts
}

func hostOnly(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if parsed, err := url.Parse(trimmed); err == nil {
		// Without the port: a route's hosts are names, and APISIX matches the Host
		// header's name.
		if host := strings.TrimSpace(parsed.Hostname()); host != "" {
			return host
		}
	}
	return strings.Trim(trimmed, "/")
}

func hasPathParams(path string) bool {
	return pathParameterPattern.MatchString(path)
}

// SidecarConfig is the config.yaml the sidecar runs with. It puts APISIX in
// standalone mode, reading its routes from apisix.yaml instead of etcd (and with
// no Admin API at all), and declares the shared dictionary the traffic cannon
// counts into: without it the plugin has nothing to count in and every load run
// fails.
//
// enable_http2 makes the gateway's port accept HTTP/2 without TLS (h2c) next to
// HTTP/1.1, which is what a gRPC client speaks: the same port serves a REST mock
// and a gRPC one. In 3.9 it is a setting of APISIX itself; the per-port form is
// deprecated and stops APISIX from starting.
//
// A suite with TCP or UDP mocks also gets the stream subsystem and a listener
// for each of its mocks: APISIX renders no stream server unless proxy_mode says
// so. The lists are written in flow style because APISIX's own YAML parser
// cannot read every nested block form.
func SidecarConfig(suite SuiteConfig) string {
	var out strings.Builder
	out.WriteString("deployment:\n")
	out.WriteString("  role: data_plane\n")
	out.WriteString("  role_data_plane:\n")
	out.WriteString("    config_provider: yaml\n")
	out.WriteString("apisix:\n")
	out.WriteString("  enable_http2: true\n")
	if endpoints := StreamEndpoints(suite); len(endpoints) > 0 {
		var tcp, udp []string
		for _, endpoint := range endpoints {
			if endpoint.Transport == "udp" {
				udp = append(udp, portString(endpoint.Port))
			} else {
				tcp = append(tcp, portString(endpoint.Port))
			}
		}
		out.WriteString("  proxy_mode: http&stream\n")
		out.WriteString("  stream_proxy:\n")
		if len(tcp) > 0 {
			out.WriteString("    tcp: [" + strings.Join(tcp, ", ") + "]\n")
		}
		if len(udp) > 0 {
			out.WriteString("    udp: [" + strings.Join(udp, ", ") + "]\n")
		}
	}
	out.WriteString("nginx_config:\n")
	out.WriteString("  http:\n")
	out.WriteString("    custom_lua_shared_dict:\n")
	out.WriteString("      " + TrafficSharedDictName + ": 50m\n")
	return out.String()
}
