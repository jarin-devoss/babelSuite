package storetest_test

import (
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/store"
	mongostore "github.com/babelsuite/babelsuite/internal/store/mongo"
	postgresstore "github.com/babelsuite/babelsuite/internal/store/postgres"
	"github.com/babelsuite/babelsuite/internal/store/storetest"
)

// startContainer runs a database image on a free loopback port and returns the
// port. The test is skipped without Docker.
func startContainer(t *testing.T, image string, containerPort int, args ...string) (string, int) {
	t.Helper()
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("docker is not available")
	}
	command := append([]string{"run", "-d", "--rm", "-p", fmt.Sprintf("127.0.0.1::%d", containerPort)}, args...)
	command = append(command, image)
	out, err := exec.Command("docker", command...).CombinedOutput()
	if err != nil {
		t.Fatalf("start %s: %v\n%s", image, err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	portOut, err := exec.Command("docker", "port", id, fmt.Sprintf("%d/tcp", containerPort)).Output()
	if err != nil {
		t.Fatalf("find the published port: %v", err)
	}
	line := strings.Split(strings.TrimSpace(string(portOut)), "\n")[0]
	_, portText, err := net.SplitHostPort(line)
	if err != nil {
		t.Fatalf("read the published port from %q: %v", line, err)
	}
	var port int
	_, _ = fmt.Sscanf(portText, "%d", &port)
	return id, port
}

func waitFor(t *testing.T, what string, try func() error) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	var last error
	for time.Now().Before(deadline) {
		if last = try(); last == nil {
			return
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("%s did not become ready: %v", what, last)
}

func TestMongoMeetsTheStoreContract(t *testing.T) {
	_, port := startContainer(t, "mongo:7.0", 27017)
	uri := fmt.Sprintf("mongodb://127.0.0.1:%d", port)
	waitFor(t, "mongo", func() error {
		s, err := mongostore.New(uri, "probe")
		if err != nil {
			return err
		}
		return s.Close(t.Context())
	})
	count := 0
	storetest.Run(t, func(t *testing.T) store.Store {
		count++
		s, err := mongostore.New(uri, fmt.Sprintf("contract_%d", count))
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}

func TestPostgresMeetsTheStoreContract(t *testing.T) {
	container, port := startContainer(t, "postgres:16", 5432, "-e", "POSTGRES_PASSWORD=pw", "-e", "POSTGRES_DB=probe")
	dsn := func(database string) string {
		return fmt.Sprintf("postgres://postgres:pw@127.0.0.1:%d/%s?sslmode=disable", port, database)
	}
	waitFor(t, "postgres", func() error {
		s, err := postgresstore.New(dsn("probe"))
		if err != nil {
			return err
		}
		return s.Close(t.Context())
	})
	count := 0
	storetest.Run(t, func(t *testing.T) store.Store {
		count++
		// A database of its own per check, so each starts empty.
		database := fmt.Sprintf("contract_%d", count)
		out, err := exec.Command("docker", "exec", "-e", "PGPASSWORD=pw", container, "psql", "-U", "postgres", "-c", "CREATE DATABASE "+database).CombinedOutput()
		if err != nil {
			t.Fatalf("create database: %v\n%s", err, out)
		}
		s, err := postgresstore.New(dsn(database))
		if err != nil {
			t.Fatal(err)
		}
		return s
	})
}
