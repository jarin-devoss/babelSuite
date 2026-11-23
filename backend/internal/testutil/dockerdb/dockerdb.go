// Package dockerdb starts a database container for a test.
//
// The image is pulled first and on its own, and only the container id is read
// back from `docker run`: when an image is not cached, `docker run` prints its
// pull progress to stderr, and reading both streams as the id (as the first
// version of these tests did) hands `docker port` a paragraph. A test that
// passed with the image cached then failed on a clean runner.
package dockerdb

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

var containerID = regexp.MustCompile(`^[0-9a-f]{12,64}$`)

// Start runs image with its containerPort published on a free loopback port and
// returns the container id and that port. The test is skipped without Docker.
// The container is removed when the test ends.
func Start(t *testing.T, image string, containerPort int, env ...string) (id string, port int) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker is not available")
	}

	if out, err := exec.Command("docker", "pull", "-q", image).CombinedOutput(); err != nil {
		t.Fatalf("pull %s: %v\n%s", image, err, out)
	}

	args := []string{"run", "-d", "--rm", "-p", fmt.Sprintf("127.0.0.1::%d", containerPort)}
	for _, pair := range env {
		args = append(args, "-e", pair)
	}
	args = append(args, image)
	var stderr strings.Builder
	command := exec.Command("docker", args...)
	command.Stderr = &stderr
	out, err := command.Output()
	if err != nil {
		t.Fatalf("start %s: %v\n%s", image, err, stderr.String())
	}
	id = lastLine(string(out))
	if !containerID.MatchString(id) {
		t.Fatalf("start %s: %q is not a container id (stdout %q, stderr %q)", image, id, out, stderr.String())
	}
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	mapped, err := exec.Command("docker", "port", id, fmt.Sprintf("%d/tcp", containerPort)).Output()
	if err != nil {
		t.Fatalf("find the published port of %s: %v", image, err)
	}
	return id, hostPort(t, string(mapped))
}

// hostPort reads a port out of `docker port` output, which has a line per
// binding (an IPv4 and an IPv6 one). The IPv4 binding is preferred.
func hostPort(t *testing.T, output string) int {
	t.Helper()
	var fallback string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		host, portText, err := net.SplitHostPort(strings.TrimSpace(line))
		if err != nil {
			continue
		}
		if ip := net.ParseIP(host); ip != nil && ip.To4() != nil {
			return atoi(t, portText)
		}
		if fallback == "" {
			fallback = portText
		}
	}
	if fallback == "" {
		t.Fatalf("no port in `docker port` output %q", output)
	}
	return atoi(t, fallback)
}

func atoi(t *testing.T, text string) int {
	t.Helper()
	value, err := strconv.Atoi(text)
	if err != nil {
		t.Fatalf("port %q: %v", text, err)
	}
	return value
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

// WaitForLog waits until the container has printed line at least n times.
// Postgres prints "database system is ready to accept connections" twice: once
// for the server that runs the init scripts and once for the real one.
func WaitForLog(t *testing.T, id, line string, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var seen int
	for time.Now().Before(deadline) {
		out, _ := exec.Command("docker", "logs", id).CombinedOutput()
		if seen = strings.Count(string(out), line); seen >= n {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("container %s did not print %q %d times in %s (saw %d)", id, line, n, timeout, seen)
}
