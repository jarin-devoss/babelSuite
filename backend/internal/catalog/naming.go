package catalog

import (
	"fmt"
	"strings"

	"github.com/babelsuite/babelsuite/internal/strutil"
)

func packageID(repository, kind string) string {
	repository = strings.TrimSpace(repository)
	// Strip registry host (anything before the first slash that looks like a host).
	if slash := strings.Index(repository, "/"); slash >= 0 {
		host := repository[:slash]
		if strings.Contains(host, ".") || strings.Contains(host, ":") || strings.EqualFold(host, "localhost") {
			repository = repository[slash+1:]
		}
	}
	// Use the last path segment as the ID (e.g. "platform/notification-hub" → "notification-hub").
	parts := strings.Split(strings.Trim(strings.ToLower(repository), "/"), "/")
	last := parts[len(parts)-1]
	if last == "" {
		last = "package"
	}
	if kind == "suite" {
		return last
	}
	return kind + "-" + last
}

func inferKind(repository string) string {
	repository = strings.ToLower(strings.TrimSpace(repository))
	if strings.HasPrefix(repository, "babelsuite/") || strings.Contains(repository, "/babelsuite/") {
		return "stdlib"
	}
	return "suite"
}

func titleForRepository(repository, kind string) string {
	repository = strings.TrimSpace(repository)
	if kind == "stdlib" {
		name := repository
		if index := strings.Index(name, "/"); index >= 0 {
			name = name[index+1:]
		}
		return "@babelsuite/" + name
	}

	parts := strings.Split(repository, "/")
	return humanize(parts[len(parts)-1])
}

func ownerForRepository(repository, fallback string) string {
	parts := strings.Split(strings.Trim(repository, "/"), "/")
	if len(parts) == 0 {
		return strutil.FirstNonEmpty(fallback, "Registry Package")
	}
	return humanize(parts[0])
}

func genericDescription(repository, registryName, kind string) string {
	if kind == "stdlib" {
		return fmt.Sprintf("Discovered in %s and treated as a BabelSuite standard library module because of its repository path.", strutil.FirstNonEmpty(registryName, "the configured registry"))
	}
	return fmt.Sprintf("Discovered directly from %s. Publish richer suite metadata inside BabelSuite to unlock deep inspect, topology, and contract views.", strutil.FirstNonEmpty(registryName, "the configured registry"))
}

func inferModules(repository string) []string {
	repository = strings.ToLower(strings.TrimSpace(repository))
	modules := make([]string, 0, 4)
	for _, candidate := range []string{"postgres", "kafka", "wiremock", "mock-api", "playwright", "grpc", "redis", "prometheus", "vault"} {
		if strings.Contains(repository, candidate) || (candidate == "mock-api" && strings.Contains(repository, "mock")) {
			modules = append(modules, candidate)
		}
	}
	return modules
}

func humanize(value string) string {
	parts := strings.FieldsFunc(strings.TrimSpace(value), func(r rune) bool {
		return r == '-' || r == '_' || r == '.'
	})
	if len(parts) == 0 {
		return value
	}
	for index, part := range parts {
		if part == strings.ToUpper(part) {
			parts[index] = part
			continue
		}
		parts[index] = strings.ToUpper(part[:1]) + strings.ToLower(part[1:])
	}
	return strings.Join(parts, " ")
}

func buildRunCommand(repository, version string) string {
	return "babelctl run " + repository + ":" + chooseVersion(nil, version)
}

func buildForkCommand(repository, version string) string {
	name := repository
	if parts := strings.Split(strings.Trim(repository, "/"), "/"); len(parts) > 0 {
		name = parts[len(parts)-1]
	}
	return "babelctl fork " + repository + ":" + chooseVersion(nil, version) + " ./" + name
}

