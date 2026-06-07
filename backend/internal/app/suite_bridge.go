package app

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/babelsuite/babelsuite/internal/apisix"
	"github.com/babelsuite/babelsuite/internal/catalog"
	"github.com/babelsuite/babelsuite/internal/platform"
	"github.com/babelsuite/babelsuite/internal/suites"
	"gopkg.in/yaml.v3"
)

type suiteSettingsReader interface {
	Load() (*platform.PlatformSettings, error)
}

type suiteWorkspaceReader interface {
	List() []suites.Definition
	Get(id string) (*suites.Definition, error)
}

type catalogSuiteReader struct {
	catalog      catalog.Reader
	settings     suiteSettingsReader
	workspace    suiteWorkspaceReader
	client       *http.Client
	mu           sync.RWMutex
	cache        map[string]*suites.Definition
	ready        chan struct{}
	moduleMu      sync.RWMutex
	moduleCache   map[string]map[string]string
	moduleByName  map[string]map[string]string
}

func newCatalogSuiteReader(cat catalog.Reader, settings suiteSettingsReader, workspace suiteWorkspaceReader) *catalogSuiteReader {
	r := &catalogSuiteReader{
		catalog:     cat,
		settings:    settings,
		workspace:   workspace,
		client:      &http.Client{Timeout: 15 * time.Second},
		cache:       make(map[string]*suites.Definition),
		ready:       make(chan struct{}),
		moduleCache:  make(map[string]map[string]string),
		moduleByName: make(map[string]map[string]string),
	}
	go func() {
		r.warmCache()
		close(r.ready)
	}()
	return r
}

func (r *catalogSuiteReader) warmCache() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	packages, err := r.catalog.ListPackages(ctx)
	if err != nil {
		return
	}

	var wg sync.WaitGroup
	for _, pkg := range packages {
		if pkg.Kind != "suite" {
			continue
		}
		wg.Add(1)
		go func(p catalog.Package) {
			defer wg.Done()
			def := r.buildDefinition(ctx, p)
			if def == nil {
				return
			}
			r.mu.Lock()
			r.cache[p.ID] = def
			r.mu.Unlock()
		}(pkg)
	}
	wg.Wait()

	cat := r.cachedCatalog()
	r.mu.Lock()
	for id, def := range r.cache {
		resolved := suites.ResolveDefinitionTopology(*def, cat)
		r.cache[id] = &resolved
	}
	r.mu.Unlock()
}

func (r *catalogSuiteReader) buildDefinition(ctx context.Context, pkg catalog.Package) *suites.Definition {
	def := suites.Definition{
		ID:          pkg.ID,
		Title:       pkg.Title,
		Description: pkg.Description,
		Repository:  pkg.Repository,
		Owner:       pkg.Owner,
		Provider:    pkg.Provider,
		Version:     pkg.Version,
		Status:      pkg.Status,
		Score:       pkg.Score,
		PullCommand: pkg.PullCommand,
		ForkCommand: pkg.ForkCommand,
		Tags:        append([]string{}, pkg.Tags...),
		Modules:     append([]string{}, pkg.Modules...),
	}

	files, err := r.pullOCIFiles(ctx, pkg)
	if err != nil || len(files) == 0 {
		return &def
	}

	applyOCIMetadata(&def, files)
	def.SuiteStar = files["suite.star"]
	def.Profiles = parseProfilesFromFiles(files)
	def.Folders = buildFoldersFromFiles(files)
	def.SourceFiles = buildSourceFilesFromMap(files)

	if gatewayYAML := buildGatewayConfig(pkg.ID, files); gatewayYAML != "" {
		def.SourceFiles = append(def.SourceFiles, suites.SourceFile{
			Path:     "gateway/apisix.yaml",
			Language: "yaml",
			Content:  gatewayYAML,
		})
		def.Folders = append(def.Folders, suites.FolderEntry{
			Name:  "gateway",
			Files: []string{"apisix.yaml"},
		})
	}

	return &def
}

func applyOCIMetadata(def *suites.Definition, files map[string]string) {
	content, ok := files["metadata.yaml"]
	if !ok || content == "" {
		return
	}
	var doc struct {
		Metadata struct {
			Title string `yaml:"title"`
		} `yaml:"metadata"`
		Spec struct {
			Description string `yaml:"description"`
			Owner       string `yaml:"owner"`
		} `yaml:"spec"`
	}
	if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
		return
	}
	if t := strings.TrimSpace(doc.Metadata.Title); t != "" {
		def.Title = t
	}
	if d := strings.TrimSpace(doc.Spec.Description); d != "" {
		def.Description = d
	}
	if o := strings.TrimSpace(doc.Spec.Owner); o != "" {
		def.Owner = o
	}
}

func (r *catalogSuiteReader) cachedCatalog() []suites.Definition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]suites.Definition, 0, len(r.cache))
	for _, def := range r.cache {
		out = append(out, *def)
	}
	return out
}

// ── APISIX gateway config generation ─────────────────────────────────────────

func buildGatewayConfig(suiteID string, files map[string]string) string {
	return apisix.RenderStandaloneConfig(apisix.SuiteConfig{
		ID:          suiteID,
		APISurfaces: parseAPISurfaces(suiteID, files),
	})
}

func parseAPISurfaces(suiteID string, files map[string]string) []apisix.SurfaceConfig {
	type rawOp struct {
		id          string
		surfaceID   string
		adapter     string
		resolverURL string
		runtimeURL  string
	}

	// Step 1 — collect operations from mock/*.metadata.yaml
	var ops []rawOp
	for path, content := range files {
		if !strings.HasPrefix(path, "mock/") || !strings.HasSuffix(path, ".metadata.yaml") {
			continue
		}
		var doc struct {
			Metadata struct {
				OperationID string `yaml:"operationId"`
			} `yaml:"metadata"`
			Spec struct {
				Adapter     string `yaml:"adapter"`
				ResolverURL string `yaml:"resolverUrl"`
				RuntimeURL  string `yaml:"runtimeUrl"`
			} `yaml:"spec"`
		}
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			continue
		}
		opID := strings.TrimSpace(doc.Metadata.OperationID)
		resolverURL := strings.TrimSpace(doc.Spec.ResolverURL)
		if opID == "" || resolverURL == "" {
			continue
		}
		// resolverUrl = /internal/mock-data/{suite}/{surface}/{op}
		parts := strings.Split(strings.Trim(resolverURL, "/"), "/")
		if len(parts) < 5 {
			continue
		}
		ops = append(ops, rawOp{
			id:          opID,
			surfaceID:   parts[3],
			adapter:     strings.ToLower(strings.TrimSpace(doc.Spec.Adapter)),
			resolverURL: resolverURL,
			runtimeURL:  strings.TrimSpace(doc.Spec.RuntimeURL),
		})
	}
	if len(ops) == 0 {
		return nil
	}

	// Step 2 — parse OpenAPI specs: operationId → {method, path, summary, host}
	type opDetail struct {
		method  string
		path    string
		summary string
		host    string
	}
	apiMap := make(map[string]opDetail)
	globalHost := ""
	for filePath, content := range files {
		if !strings.HasPrefix(filePath, "api/") {
			continue
		}
		low := strings.ToLower(filePath)
		if !strings.HasSuffix(low, ".yaml") && !strings.HasSuffix(low, ".yml") {
			continue
		}
		var doc struct {
			Servers []struct {
				URL string `yaml:"url"`
			} `yaml:"servers"`
			Paths map[string]map[string]struct {
				OperationID string `yaml:"operationId"`
				Summary     string `yaml:"summary"`
			} `yaml:"paths"`
		}
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			continue
		}
		host := ""
		if len(doc.Servers) > 0 {
			host = strings.TrimSpace(doc.Servers[0].URL)
			if globalHost == "" {
				globalHost = host
			}
		}
		for apiPath, methods := range doc.Paths {
			for httpMethod, op := range methods {
				key := normalizeOpKey(op.OperationID)
				if key == "" {
					continue
				}
				apiMap[key] = opDetail{
					method:  strings.ToUpper(httpMethod),
					path:    apiPath,
					summary: strings.TrimSpace(op.Summary),
					host:    host,
				}
			}
		}
	}

	// Step 3 — collect proto files (path → content)
	protoFiles := make(map[string]string)
	for filePath, content := range files {
		if strings.HasPrefix(filePath, "api/") && strings.HasSuffix(filePath, ".proto") {
			protoFiles[filePath] = content
		}
	}

	// Step 4 — detect SOAP (WSDL present anywhere in api/)
	hasWSDL := false
	for filePath := range files {
		if strings.HasSuffix(strings.ToLower(filePath), ".wsdl") {
			hasWSDL = true
			break
		}
	}

	// Step 5 — group operations by surface, preserving first-seen order
	type surfaceGroup struct {
		adapter string
		ops     []rawOp
	}
	groupMap := make(map[string]*surfaceGroup)
	var order []string
	for _, op := range ops {
		if _, ok := groupMap[op.surfaceID]; !ok {
			groupMap[op.surfaceID] = &surfaceGroup{adapter: op.adapter}
			order = append(order, op.surfaceID)
		}
		groupMap[op.surfaceID].ops = append(groupMap[op.surfaceID].ops, op)
	}

	// Step 6 — build apisix.SurfaceConfig for each group
	var result []apisix.SurfaceConfig
	for _, surfaceID := range order {
		grp := groupMap[surfaceID]
		protocol := gatewayProtocol(grp.adapter, hasWSDL)
		mockHost := ""

		var apiOps []apisix.OperationConfig
		for _, op := range grp.ops {
			detail := apiMap[normalizeOpKey(op.id)]
			if mockHost == "" && detail.host != "" {
				mockHost = detail.host
			}

			method := detail.method
			name := detail.path
			summary := detail.summary
			if method == "" {
				method = gatewayDefaultMethod(op.adapter)
			}
			if name == "" {
				name = "/" + op.id
			}

			contractPath, contractContent := "", ""
			if op.adapter == "grpc" {
				contractPath, contractContent = protoForOperation(op.id, protoFiles)
			}

			apiOps = append(apiOps, apisix.OperationConfig{
				ID:              op.id,
				Method:          method,
				Name:            name,
				Summary:         summary,
				ContractPath:    contractPath,
				ContractContent: contractContent,
				MockMetadata: apisix.OperationMetadataConfig{
					Adapter:     op.adapter,
					ResolverURL: op.resolverURL,
					RuntimeURL:  op.runtimeURL,
				},
			})
		}

		if mockHost == "" {
			mockHost = globalHost
		}
		result = append(result, apisix.SurfaceConfig{
			ID:         surfaceID,
			Protocol:   protocol,
			MockHost:   mockHost,
			Operations: apiOps,
		})
	}
	return result
}

// normalizeOpKey strips dashes, underscores, and lowercases so "create_payment"
// and "create-payment" match the same OpenAPI operationId.
func normalizeOpKey(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.ReplaceAll(id, "-", "")
	id = strings.ReplaceAll(id, "_", "")
	return id
}

func gatewayProtocol(adapter string, hasWSDL bool) string {
	switch adapter {
	case "grpc":
		return "gRPC"
	case "async":
		return "Async"
	default:
		if hasWSDL {
			return "SOAP"
		}
		return "REST"
	}
}

func gatewayDefaultMethod(adapter string) string {
	switch adapter {
	case "grpc":
		return "RPC"
	case "async":
		return "EVENT"
	default:
		return "POST"
	}
}

// protoForOperation finds the proto file whose content contains a matching rpc
// method (derived by converting operationId to PascalCase) and returns the
// contractPath + content needed by the gRPC renderer.
func protoForOperation(operationID string, protoFiles map[string]string) (contractPath, content string) {
	method := toPascalCase(operationID)
	for filePath, fc := range protoFiles {
		if strings.Contains(fc, "rpc "+method) {
			return filePath + "#" + method, fc
		}
	}
	// fallback: first proto, no method anchor
	for filePath, fc := range protoFiles {
		return filePath, fc
	}
	return "", ""
}

func toPascalCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, "")
}

func (r *catalogSuiteReader) List() []suites.Definition {
	r.mu.RLock()
	result := make([]suites.Definition, 0, len(r.cache))
	for _, def := range r.cache {
		result = append(result, *def)
	}
	r.mu.RUnlock()

	if r.workspace != nil {
		catalogIDs := make(map[string]struct{}, len(result))
		for _, d := range result {
			catalogIDs[d.ID] = struct{}{}
		}
		for _, d := range r.workspace.List() {
			if _, exists := catalogIDs[d.ID]; !exists {
				result = append(result, d)
			}
		}
	}

	return result
}

func (r *catalogSuiteReader) Get(id string) (*suites.Definition, error) {
	id = strings.TrimSpace(id)

	r.mu.RLock()
	cached := r.cache[id]
	r.mu.RUnlock()
	if cached != nil {
		clone := *cached
		return &clone, nil
	}

	pkg, err := r.catalog.GetPackage(context.Background(), id)
	if err != nil {
		if r.workspace != nil {
			return r.workspace.Get(id)
		}
		return nil, suites.ErrNotFound
	}

	def := r.buildDefinition(context.Background(), *pkg)
	if def == nil {
		return nil, suites.ErrNotFound
	}

	resolved := suites.ResolveDefinitionTopology(*def, r.cachedCatalog())
	r.mu.Lock()
	r.cache[id] = &resolved
	r.mu.Unlock()

	clone := resolved
	return &clone, nil
}

func (r *catalogSuiteReader) Resolve(ref string) (*suites.Definition, error) {
	return r.Get(ref)
}

func (r *catalogSuiteReader) ResolveModuleFiles(name string) (map[string]string, error) {
	r.moduleMu.RLock()
	if cached, ok := r.moduleByName[name]; ok {
		r.moduleMu.RUnlock()
		return cached, nil
	}
	r.moduleMu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	packages, err := r.catalog.ListPackages(ctx)
	if err != nil {
		return nil, err
	}
	needle := "/babelsuite/" + name
	for _, pkg := range packages {
		if pkg.Kind == "stdlib" && strings.HasSuffix(strings.ToLower(pkg.Repository), strings.ToLower(needle)) {
			files, err := r.pullOCIFiles(ctx, pkg)
			if err != nil {
				return nil, err
			}
			r.moduleMu.Lock()
			r.moduleByName[name] = files
			r.moduleMu.Unlock()
			return files, nil
		}
	}
	return nil, fmt.Errorf("module @babelsuite/%s not found in catalog", name)
}

// ── Suite handler adapter ─────────────────────────────────────────────────────

type suiteRegistrar interface {
	Register(req suites.RegisterRequest) (suites.Definition, error)
}

type catalogSuiteHandler struct {
	reader    *catalogSuiteReader
	registrar suiteRegistrar
}

func newCatalogSuiteHandler(reader *catalogSuiteReader, registrar suiteRegistrar) *catalogSuiteHandler {
	return &catalogSuiteHandler{reader: reader, registrar: registrar}
}

func (h *catalogSuiteHandler) List() []suites.Definition { return h.reader.List() }
func (h *catalogSuiteHandler) Get(id string) (*suites.Definition, error) {
	return h.reader.Get(id)
}
func (h *catalogSuiteHandler) Register(req suites.RegisterRequest) (suites.Definition, error) {
	return h.registrar.Register(req)
}

// ── OCI layer pulling ─────────────────────────────────────────────────────────

type ociManifest struct {
	Layers []struct {
		Digest string `json:"digest"`
		Size   int    `json:"size"`
	} `json:"layers"`
}

func (r *catalogSuiteReader) pullOCIFiles(ctx context.Context, pkg catalog.Package) (map[string]string, error) {
	host, repoPath, err := splitRepository(pkg.Repository)
	if err != nil {
		return nil, err
	}

	version := strings.TrimSpace(pkg.Version)
	if version == "" {
		version = "latest"
	}

	reg := r.findRegistry(host)
	baseURL := "http://" + host
	if reg != nil && strings.TrimSpace(reg.RegistryURL) != "" {
		baseURL = strings.TrimRight(reg.RegistryURL, "/")
	}

	manifest, err := r.fetchManifest(ctx, baseURL, repoPath, version, reg)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	if len(manifest.Layers) == 0 {
		return nil, fmt.Errorf("no layers in manifest")
	}

	digest := manifest.Layers[0].Digest

	r.moduleMu.RLock()
	cached, hit := r.moduleCache[digest]
	r.moduleMu.RUnlock()
	if hit {
		return cached, nil
	}

	files, err := r.fetchAndExtractLayer(ctx, baseURL, repoPath, digest, reg)
	if err != nil {
		return nil, err
	}

	r.moduleMu.Lock()
	r.moduleCache[digest] = files
	r.moduleMu.Unlock()

	return files, nil
}

func (r *catalogSuiteReader) fetchManifest(ctx context.Context, baseURL, repoPath, tag string, reg *platform.OCIRegistry) (*ociManifest, error) {
	target := baseURL + "/v2/" + encodeRepoPath(repoPath) + "/manifests/" + url.PathEscape(tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.oci.image.manifest.v1+json")
	applyAuth(req, reg)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("manifest returned %s", resp.Status)
	}

	var m ociManifest
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *catalogSuiteReader) fetchAndExtractLayer(ctx context.Context, baseURL, repoPath, digest string, reg *platform.OCIRegistry) (map[string]string, error) {
	target := baseURL + "/v2/" + encodeRepoPath(repoPath) + "/blobs/" + digest
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	applyAuth(req, reg)

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("blob returned %s", resp.Status)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	files := make(map[string]string)
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, 2<<20))
		if err != nil {
			return nil, err
		}
		files[hdr.Name] = string(data)
	}
	return files, nil
}

func (r *catalogSuiteReader) findRegistry(host string) *platform.OCIRegistry {
	if r.settings == nil {
		return nil
	}
	settings, err := r.settings.Load()
	if err != nil || settings == nil {
		return nil
	}
	for i := range settings.Registries {
		reg := &settings.Registries[i]
		regURL := strings.TrimRight(strings.TrimSpace(reg.RegistryURL), "/")
		if strings.HasSuffix(regURL, "://"+host) || strings.HasSuffix(regURL, host) {
			return reg
		}
	}
	return nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func splitRepository(repository string) (host, path string, err error) {
	repository = strings.TrimSpace(repository)
	if repository == "" {
		return "", "", fmt.Errorf("empty repository")
	}
	if strings.Contains(repository, "://") {
		parsed, e := url.Parse(repository)
		if e != nil {
			return "", "", e
		}
		return parsed.Host, strings.Trim(parsed.Path, "/"), nil
	}
	slash := strings.Index(repository, "/")
	if slash < 0 {
		return "", "", fmt.Errorf("repository %q has no path", repository)
	}
	candidate := repository[:slash]
	if strings.Contains(candidate, ".") || strings.Contains(candidate, ":") || strings.EqualFold(candidate, "localhost") {
		return candidate, strings.Trim(repository[slash+1:], "/"), nil
	}
	return "", "", fmt.Errorf("cannot determine registry host from %q", repository)
}

func encodeRepoPath(repoPath string) string {
	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	for i := range parts {
		parts[i] = url.PathEscape(parts[i])
	}
	return strings.Join(parts, "/")
}

func applyAuth(req *http.Request, reg *platform.OCIRegistry) {
	if reg == nil {
		return
	}
	user := strings.TrimSpace(reg.Username)
	secret := strings.TrimSpace(reg.Secret)
	if user != "" && secret != "" && !strings.Contains(secret, "://") {
		req.SetBasicAuth(user, secret)
	}
}

func buildSourceFilesFromMap(files map[string]string) []suites.SourceFile {
	skip := map[string]bool{"metadata.yaml": true}
	result := make([]suites.SourceFile, 0, len(files))
	for path, content := range files {
		if skip[path] {
			continue
		}
		result = append(result, suites.SourceFile{
			Path:     path,
			Language: suites.DetectSourceLanguage(path),
			Content:  content,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func buildFoldersFromFiles(files map[string]string) []suites.FolderEntry {
	grouped := make(map[string][]string)
	for path := range files {
		if path == "babelsuite-api.json" {
			continue
		}
		slash := strings.Index(path, "/")
		if slash < 0 {
			continue
		}
		dir := path[:slash]
		rel := path[slash+1:]
		if dir == "" || rel == "" {
			continue
		}
		grouped[dir] = append(grouped[dir], rel)
	}

	folders := make([]suites.FolderEntry, 0, len(grouped))
	for dir, fileList := range grouped {
		sort.Strings(fileList)
		folders = append(folders, suites.FolderEntry{Name: dir, Files: fileList})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders
}

func parseProfilesFromFiles(files map[string]string) []suites.ProfileOption {
	type profileDoc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Default     bool   `yaml:"default"`
		Runtime     struct {
			ProfileFile string `yaml:"profileFile"`
		} `yaml:"runtime"`
	}

	profiles := make([]suites.ProfileOption, 0)
	for path, content := range files {
		if !strings.HasPrefix(path, "profiles/") {
			continue
		}
		if !strings.HasSuffix(path, ".yaml") && !strings.HasSuffix(path, ".yml") {
			continue
		}
		fileName := path[len("profiles/"):]

		var doc profileDoc
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			continue
		}

		label := strings.TrimSpace(doc.Name)
		if label == "" {
			label = humanizeFileName(strings.TrimSuffix(strings.TrimSuffix(fileName, ".yaml"), ".yml"))
		}
		profileFile := strings.TrimSpace(doc.Runtime.ProfileFile)
		if profileFile == "" {
			profileFile = fileName
		}
		profiles = append(profiles, suites.ProfileOption{
			FileName:    profileFile,
			Label:       label,
			Description: strings.TrimSpace(doc.Description),
			Default:     doc.Default,
		})
	}

	for i, p := range profiles {
		if p.Default && i != 0 {
			profiles[0], profiles[i] = profiles[i], profiles[0]
			break
		}
	}
	return profiles
}

func humanizeFileName(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '-' || r == '_' })
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + strings.ToLower(p[1:])
		}
	}
	return strings.Join(parts, " ")
}

