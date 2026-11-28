package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/babelsuite/babelsuite/internal/catalog"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
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

	yaml := buildGatewayConfig("payment-suite", files, nil)
	if yaml == "" {
		t.Fatal("expected gateway config, got empty string")
	}

	checks := []string{
		"id: payment-suite.create-payment",
		"id: payment-suite.get-payment",
		"uri: /payments",
		"uri: /payments/*",
		// The gateway's plugin answers: it is told which operation it serves and
		// asks the mock engine for the example, with the engine and token the
		// sidecar resolves from its environment.
		"babelsuite-mock:",
		"operation: create-payment",
		"surface: payment-gateway",
		"engine: ${{BABELSUITE_ENGINE_ADDR}}",
		"token: ${{BABELSUITE_MOCK_TOKEN}}",
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

	yaml := buildGatewayConfig("returns-control-plane", files, nil)
	if yaml == "" {
		t.Fatal("expected gateway config for returns-control-plane")
	}

	// A gRPC mock is served by the control plane's gRPC front end, which has the
	// suite's .proto files: the gateway forwards every gRPC call of the suite to
	// it, and does not transcode.
	for _, want := range []string{
		"id: returns-control-plane.grpc",
		"scheme: grpc",
		"${{BABELSUITE_ENGINE_GRPC_ADDR}}",
		"id: returns-control-plane.grpc-web",
	} {
		if !strings.Contains(yaml, want) {
			t.Errorf("expected %q in the gRPC suite's gateway config", want)
		}
	}
	if strings.Contains(yaml, "grpc-transcode") || strings.Contains(yaml, "protos:") {
		t.Error("the gateway must not carry protos or transcode: the front end owns them")
	}
}

func TestBuildGatewayConfigSOAPSuite(t *testing.T) {
	files := loadExampleSuite(t, filepath.Join(examplesDir(t), "oci-suites", "soap-claims-hub"))

	yaml := buildGatewayConfig("soap-claims-hub", files, nil)
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
		"suite.star":          `api = service.run(image="alpine:3.19", name="api")`,
		"profiles/local.yaml": "name: Local\ndefault: true\n",
	}
	result := buildGatewayConfig("no-meta-suite", files, nil)
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

// A pushed suite's surface takes its protocol from its operations' adapters: a
// gRPC mock was always labelled REST, so the gateway built it a REST route.
func TestAnOCISuitesGRPCMockIsNotLabelledREST(t *testing.T) {
	files := map[string]string{
		"mock/pricing/quote.metadata.yaml": "metadata:\n  operationId: quote-refund\n  sourceArtifact: mock/pricing/quote.cue\nspec:\n  adapter: grpc\n  resolverUrl: /internal/mock-data/demo/pricing-service/quote-refund\n",
		"mock/returns/get.metadata.yaml":   "metadata:\n  operationId: get-return\n  sourceArtifact: mock/returns/get.cue\nspec:\n  adapter: rest\n  resolverUrl: /internal/mock-data/demo/returns-api/get-return\n",
	}
	got := map[string]string{}
	for _, surface := range suites.SurfacesFromFiles(files) {
		got[surface.ID] = surface.Protocol
	}
	if got["pricing-service"] != "gRPC" || got["returns-api"] != "REST" {
		t.Fatalf("protocols = %v, want pricing-service gRPC and returns-api REST", got)
	}
}

// The directory loader (what tests use) and the OCI loader (what runs) build a
// suite's surfaces, profiles and folders from the same functions, so a suite looks
// the same however it was loaded. They used to be separate code, and tests saw
// surfaces with no protocol or host.
func TestBothLoadersAgreeOnEveryExample(t *testing.T) {
	workspace := suites.NewWorkspaceService()
	root := filepath.Join("..", "..", "..", "examples", "oci-suites")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		t.Run(name, func(t *testing.T) {
			def, err := workspace.Get(name)
			if err != nil {
				t.Skip("not a workspace suite")
			}
			files := loadExampleSuite(t, filepath.Join(root, name))
			// The catalog normalizes a definition either loader built.
			oci := suites.NormalizeDefinition(suites.Definition{
				ID:          name,
				APISurfaces: suites.SurfacesFromFiles(files),
				Folders:     suites.FoldersFromFiles(files),
			})
			if !sameJSON(oci.APISurfaces, def.APISurfaces) {
				t.Errorf("surfaces differ:\n oci:       %+v\n workspace: %+v", oci.APISurfaces, def.APISurfaces)
			}
			if !sameJSON(oci.Folders, def.Folders) {
				t.Errorf("folders differ:\n oci:       %+v\n workspace: %+v", oci.Folders, def.Folders)
			}
			if got, want := suites.ProfilesFromFiles(files), def.Profiles; !profilesEqual(got, want) {
				t.Errorf("profiles differ:\n oci:       %+v\n workspace: %+v", got, want)
			}
		})
	}
}

func profilesEqual(a, b []suites.ProfileOption) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		a[i].Content, b[i].Content = "", ""
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameJSON compares two values as the API would send them, so an empty list and a
// missing one are the same.
func sameJSON(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return strings.ReplaceAll(string(left), "null", "[]") == strings.ReplaceAll(string(right), "null", "[]")
}

// fakeCatalog lists the packages it is given, and can hold a listing back.
type fakeCatalog struct {
	mu       sync.Mutex
	packages []catalog.Package
	hold     chan struct{} // when set, ListPackages waits for it to close
	started  chan struct{} // closed when a held listing has begun
}

func (f *fakeCatalog) ListPackages(ctx context.Context) ([]catalog.Package, error) {
	f.mu.Lock()
	hold, started := f.hold, f.started
	packages := append([]catalog.Package{}, f.packages...)
	f.mu.Unlock()
	if hold != nil {
		close(started)
		select {
		case <-hold:
		case <-ctx.Done():
		}
	}
	return packages, nil
}

func (f *fakeCatalog) GetPackage(_ context.Context, id string) (*catalog.Package, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.packages {
		if p.ID == id {
			clone := p
			return &clone, nil
		}
	}
	return nil, suites.ErrNotFound
}

type emptySettings struct{}

func (emptySettings) Load() (*platform.PlatformSettings, error) {
	return &platform.PlatformSettings{}, nil
}

func suiteIDs(defs []suites.Definition) string {
	ids := make([]string, 0, len(defs))
	for _, d := range defs {
		ids = append(ids, d.ID)
	}
	sort.Strings(ids)
	return strings.Join(ids, ",")
}

// A registry sync rebuilds the catalog beside the old one and swaps it in whole.
// A run that starts while the registries are being read must still see every
// suite, as it was before the push; it used to see an empty catalog and failed
// with "file not found" for an imported suite's service.
func TestARefreshNeverShowsAnEmptyCatalog(t *testing.T) {
	cat := &fakeCatalog{packages: []catalog.Package{{ID: "alpha", Kind: "suite"}, {ID: "beta", Kind: "suite"}}}
	reader := newCatalogSuiteReader(cat, emptySettings{}, nil)
	reader.waitReady()
	if got := suiteIDs(reader.List()); got != "alpha,beta" {
		t.Fatalf("before the refresh: %q, want alpha,beta", got)
	}

	cat.mu.Lock()
	cat.hold, cat.started = make(chan struct{}), make(chan struct{})
	cat.packages = []catalog.Package{{ID: "alpha", Kind: "suite"}, {ID: "gamma", Kind: "suite"}}
	hold, started := cat.hold, cat.started
	cat.mu.Unlock()

	done := make(chan struct{})
	go func() { reader.Refresh(); close(done) }()
	<-started

	// The registries are mid-read: the old catalog still answers, whole.
	if got := suiteIDs(reader.List()); got != "alpha,beta" {
		t.Errorf("during the refresh: %q, want the old catalog alpha,beta", got)
	}
	if _, err := reader.Get("beta"); err != nil {
		t.Errorf("during the refresh, Get(beta): %v", err)
	}

	close(hold)
	<-done
	if got := suiteIDs(reader.List()); got != "alpha,gamma" {
		t.Errorf("after the refresh: %q, want alpha,gamma (beta removed, gamma added)", got)
	}
}

// Reads wait for the first load of the registries, and the catalog says when it
// has finished, so /readyz does not report a control plane ready before it knows
// its suites.
func TestTheCatalogIsReadyOnlyAfterItsFirstRead(t *testing.T) {
	cat := &fakeCatalog{
		packages: []catalog.Package{{ID: "alpha", Kind: "suite"}},
		hold:     make(chan struct{}),
		started:  make(chan struct{}),
	}
	reader := newCatalogSuiteReader(cat, emptySettings{}, nil)
	<-cat.started
	if reader.Ready() {
		t.Fatal("the catalog reported ready before its first read finished")
	}

	listed := make(chan string, 1)
	go func() { listed <- suiteIDs(reader.List()) }()
	select {
	case got := <-listed:
		t.Fatalf("List answered %q before the first read finished", got)
	case <-time.After(150 * time.Millisecond):
	}

	close(cat.hold)
	if got := <-listed; got != "alpha" {
		t.Errorf("List after the first read: %q, want alpha", got)
	}
	if !reader.Ready() {
		t.Error("the catalog is not ready after its first read")
	}
}

type flagReady struct{ ready bool }

func (f *flagReady) Ready() bool { return f.ready }

func TestTheCatalogProbeIsNotReadyUntilTheCatalogIs(t *testing.T) {
	flag := &flagReady{}
	check, err := catalogProbe(flag)(context.Background())
	if err != nil || check.Status != probeNotReady {
		t.Fatalf("before the first read: %+v, %v; want not ready", check, err)
	}
	flag.ready = true
	if check, _ = catalogProbe(flag)(context.Background()); check.Status != probeReady {
		t.Fatalf("after the first read: %+v; want ready", check)
	}
}
