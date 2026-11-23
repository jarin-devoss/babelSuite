package storetest_test

import (
	"fmt"
	"os/exec"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/store"
	mongostore "github.com/babelsuite/babelsuite/internal/store/mongo"
	postgresstore "github.com/babelsuite/babelsuite/internal/store/postgres"
	"github.com/babelsuite/babelsuite/internal/store/storetest"
	"github.com/babelsuite/babelsuite/internal/testutil/dockerdb"
)

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
	id, port := dockerdb.Start(t, "mongo:7.0", 27017)
	dockerdb.WaitForLog(t, id, "Waiting for connections", 1, 2*time.Minute)
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
	container, port := dockerdb.Start(t, "postgres:16", 5432, "POSTGRES_PASSWORD=pw", "POSTGRES_DB=probe")
	dockerdb.WaitForLog(t, container, "database system is ready to accept connections", 2, 2*time.Minute)
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
