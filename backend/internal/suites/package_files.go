package suites

import (
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// What a suite package's files say, read from a map of path to content. The
// directory loader (workspace.go) and the OCI loader in the control plane both
// build a Definition from these, so the two cannot drift apart.

// SurfacesFromFiles builds fully-populated APISurface objects from the OCI
// file map. It reads mock/*.metadata.yaml for MockPath, MockMetadata (adapter,
// fallback, constraints, delay) and api/*.yaml for method/path/summary/host.
// Both loaders of a suite package, the one that reads a directory and the one that
// pulls an OCI layer, call it, so a suite has the same surfaces however it was loaded.
func SurfacesFromFiles(files map[string]string) []APISurface {
	type rawOp struct {
		surfaceID string
		op        APIOperation
	}

	// Step 1 — parse mock/*.metadata.yaml for full operation metadata.
	var ops []rawOp
	for _, path := range sortedPaths(files) {
		content := files[path]
		if !strings.HasPrefix(path, "mock/") || !strings.HasSuffix(path, ".metadata.yaml") {
			continue
		}
		var doc struct {
			Metadata struct {
				OperationID    string `yaml:"operationId"`
				SourceArtifact string `yaml:"sourceArtifact"`
			} `yaml:"metadata"`
			Spec struct {
				Adapter              string                       `yaml:"adapter"`
				DelayMillis          int                          `yaml:"delayMillis"`
				ResolverURL          string                       `yaml:"resolverUrl"`
				RuntimeURL           string                       `yaml:"runtimeUrl"`
				ParameterConstraints []ParameterConstraint `yaml:"parameterConstraints"`
				Fallback             *MockFallback         `yaml:"fallback"`
				State                *MockState            `yaml:"state"`
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
		parts := strings.Split(strings.Trim(resolverURL, "/"), "/")
		if len(parts) < 5 {
			continue
		}
		ops = append(ops, rawOp{
			surfaceID: parts[3],
			op: APIOperation{
				ID:       opID,
				MockPath: strings.TrimSpace(doc.Metadata.SourceArtifact),
				MockMetadata: MockOperationMetadata{
					Adapter:              strings.ToLower(strings.TrimSpace(doc.Spec.Adapter)),
					DelayMillis:          doc.Spec.DelayMillis,
					ResolverURL:          resolverURL,
					RuntimeURL:           strings.TrimSpace(doc.Spec.RuntimeURL),
					ParameterConstraints: doc.Spec.ParameterConstraints,
					Fallback:             doc.Spec.Fallback,
					State:                doc.Spec.State,
					MetadataPath:         path,
				},
			},
		})
	}
	if len(ops) == 0 {
		return nil
	}

	// Step 2 — parse api/*.yaml OpenAPI specs: operationId → {method, path, summary, host}.
	type opDetail struct {
		method  string
		path    string
		summary string
		host    string
	}
	apiMap := make(map[string]opDetail)
	globalHost := ""
	for _, filePath := range sortedPaths(files) {
		content := files[filePath]
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
				key := NormalizeOpKey(op.OperationID)
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

	// Step 3 — apply method/path/summary from OpenAPI; fall back to runtimeUrl derivation.
	for i := range ops {
		detail := apiMap[NormalizeOpKey(ops[i].op.ID)]
		if detail.method != "" {
			ops[i].op.Method = detail.method
			ops[i].op.Name = detail.path
			ops[i].op.Summary = detail.summary
		} else {
			ru := ops[i].op.MockMetadata.RuntimeURL
			if idx := strings.Index(ru, "?"); idx >= 0 {
				ru = ru[:idx]
			}
			parts := strings.SplitN(ru, "/", 6)
			if len(parts) >= 6 {
				ops[i].op.Name = "/" + parts[5]
			}
			ops[i].op.Method = "POST"
		}
	}

	// Step 4 — group by surfaceID preserving first-seen order.
	type surfaceGroup struct {
		ops  []APIOperation
		host string
	}
	groupMap := make(map[string]*surfaceGroup)
	var order []string
	for _, raw := range ops {
		if _, ok := groupMap[raw.surfaceID]; !ok {
			groupMap[raw.surfaceID] = &surfaceGroup{}
			order = append(order, raw.surfaceID)
		}
		grp := groupMap[raw.surfaceID]
		grp.ops = append(grp.ops, raw.op)
		if grp.host == "" {
			if d := apiMap[NormalizeOpKey(raw.op.ID)]; d.host != "" {
				grp.host = d.host
			}
		}
	}

	// Step 5 — build surfaces.
	result := make([]APISurface, 0, len(order))
	for _, surfaceID := range order {
		grp := groupMap[surfaceID]
		host := grp.host
		if host == "" {
			host = globalHost
		}
		result = append(result, APISurface{
			ID:         surfaceID,
			Protocol:   surfaceProtocol(grp.ops),
			MockHost:   host,
			Operations: grp.ops,
		})
	}
	return result
}

// NormalizeOpKey strips dashes, underscores, and lowercases so "create_payment"
// and "create-payment" match the same OpenAPI operationId.
func NormalizeOpKey(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	id = strings.ReplaceAll(id, "-", "")
	id = strings.ReplaceAll(id, "_", "")
	return id
}

// surfaceProtocol names the protocol of a surface from the adapters of its
// operations. It was always "REST", so a gRPC or async mock of a pushed suite was
// built into a REST route and never served as what it is.
func surfaceProtocol(operations []APIOperation) string {
	for _, operation := range operations {
		switch strings.ToLower(strings.TrimSpace(operation.MockMetadata.Adapter)) {
		case "grpc":
			return "gRPC"
		case "async":
			return "Async"
		case "websocket":
			return "WebSocket"
		case "graphql":
			return "GraphQL"
		case "soap":
			return "SOAP"
		}
	}
	return "REST"
}
func ProfilesFromFiles(files map[string]string) []ProfileOption {
	type profileDoc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
		Default     bool   `yaml:"default"`
		Runtime     struct {
			ProfileFile string `yaml:"profileFile"`
		} `yaml:"runtime"`
	}

	profiles := make([]ProfileOption, 0)
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
		profiles = append(profiles, ProfileOption{
			FileName:    profileFile,
			Label:       label,
			Description: strings.TrimSpace(doc.Description),
			Default:     doc.Default,
		})
	}

	sort.Slice(profiles, func(i, j int) bool {
		if profiles[i].Default != profiles[j].Default {
			return profiles[i].Default
		}
		return profiles[i].FileName < profiles[j].FileName
	})
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


func SourceFilesFromMap(files map[string]string) []SourceFile {
	skip := map[string]bool{"metadata.yaml": true}
	result := make([]SourceFile, 0, len(files))
	for path, content := range files {
		if skip[path] {
			continue
		}
		result = append(result, SourceFile{
			Path:     path,
			Language: DetectSourceLanguage(path),
			Content:  content,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result
}

func FoldersFromFiles(files map[string]string) []FolderEntry {
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

	folders := make([]FolderEntry, 0, len(grouped))
	for dir, fileList := range grouped {
		sort.Strings(fileList)
		folders = append(folders, FolderEntry{Name: dir, Role: folderRole(dir), Description: folderDescription(dir), Files: fileList})
	}
	sort.Slice(folders, func(i, j int) bool { return folders[i].Name < folders[j].Name })
	return folders
}

// sortedPaths lists the files of a package in path order, so what is built from
// them does not depend on map iteration.
func sortedPaths(files map[string]string) []string {
	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}
