package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadExampleSuite(t *testing.T, suiteDir string) map[string]string {
	t.Helper()
	files := make(map[string]string)
	err := filepath.WalkDir(suiteDir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(suiteDir, path)
		rel = filepath.ToSlash(rel)
		if rel == "metadata.yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[rel] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("load suite dir %s: %v", suiteDir, err)
	}
	return files
}

func examplesDir(t *testing.T) string {
	t.Helper()
	// Walk up from this file to the repo root, then into examples/
	dir, err := filepath.Abs("../../../examples")
	if err != nil {
		t.Fatalf("resolve examples dir: %v", err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("examples dir not found at %s, skipping", dir)
	}
	return dir
}

func TestBuildGatewayConfigPaymentSuiteRESTRoutes(t *testing.T) {
	files := loadExampleSuite(t, filepath.Join(examplesDir(t), "oci-suites", "payment-suite"))

	yaml := buildGatewayConfig("payment-suite", files)
	if yaml == "" {
		t.Fatal("expected gateway config, got empty string")
	}

	checks := []string{
		"id: payment-suite.create-payment",
		"id: payment-suite.get-payment",
		"uri: /payments",
		"uri: /payments/*",
		"/internal/mock-data/payment-suite/payment-gateway/create-payment",
		"X-Babelsuite-Dispatcher: apisix",
		"hosts:",
		"payment-suite.mock.internal",
		"babelsuite-traffic-trigger",
		"#END",
	}
	for _, want := range checks {
		if !strings.Contains(yaml, want) {
			t.Errorf("expected %q in gateway config, got:\n%s", want, yaml)
		}
	}
}

func TestBuildGatewayConfigReturnsControlPlaneGRPC(t *testing.T) {
	files := loadExampleSuite(t, filepath.Join(examplesDir(t), "oci-suites", "returns-control-plane"))

	yaml := buildGatewayConfig("returns-control-plane", files)
	if yaml == "" {
		t.Fatal("expected gateway config for returns-control-plane")
	}

	if !strings.Contains(yaml, "protos:") {
		t.Error("expected protos section for gRPC suite")
	}
	if !strings.Contains(yaml, "grpc-transcode:") {
		t.Error("expected grpc-transcode plugin for gRPC operation")
	}
	if !strings.Contains(yaml, "scheme: grpc") {
		t.Error("expected grpc upstream scheme")
	}
}

func TestBuildGatewayConfigSOAPSuite(t *testing.T) {
	files := loadExampleSuite(t, filepath.Join(examplesDir(t), "oci-suites", "soap-claims-hub"))

	yaml := buildGatewayConfig("soap-claims-hub", files)
	if yaml == "" {
		t.Fatal("expected gateway config for soap-claims-hub")
	}

	if !strings.Contains(yaml, "soap-claims-hub.claim-service") {
		t.Error("expected claim-service route")
	}
}

func TestBuildGatewayConfigAlwaysIncludesTrafficCannon(t *testing.T) {
	// Even with no mock metadata, every suite gets a base gateway config
	// with the traffic cannon sidecar route so load tests and security scans work.
	files := map[string]string{
		"suite.star":          `api = service.run(name="api")`,
		"profiles/local.yaml": "name: Local\ndefault: true\n",
	}
	result := buildGatewayConfig("no-meta-suite", files)
	if result == "" {
		t.Fatal("expected base gateway config even with no mock metadata")
	}
	if !strings.Contains(result, "babelsuite-traffic-trigger") {
		t.Error("expected traffic cannon trigger route in base config")
	}
	if !strings.Contains(result, "#END") {
		t.Error("expected #END terminator in config")
	}
}
