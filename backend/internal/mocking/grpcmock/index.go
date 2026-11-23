// Package grpcmock serves a suite's gRPC mocks.
//
// A gRPC client calls /pkg.Service/Method, and the gateway of the suite forwards
// every such call here. The suite's .proto files are compiled at run time (no
// generated code), the call is matched to the mock operation that declares the
// method, the request is decoded into JSON for the mock engine, and the engine's
// answer is turned back into the method's output message or a gRPC status.
package grpcmock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"

	"github.com/bufbuild/protocompile"
	"github.com/bufbuild/protocompile/linker"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/babelsuite/babelsuite/internal/suites"
)

// Problem says why one gRPC mock operation of a suite cannot be served.
type Problem struct {
	Surface   string
	Operation string
	Reason    string
}

// method is one gRPC method a suite mocks.
type method struct {
	surface   string
	operation string
	desc      protoreflect.MethodDescriptor
}

// Index is the compiled view of a suite's gRPC mocks.
type Index struct {
	methods  map[string]method // "/pkg.Service/Method" -> operation
	files    linker.Files
	problems []Problem
	// byPath finds a compiled file by its path, for reflection.
	byPath map[string]protoreflect.FileDescriptor
}

// Problems lists the gRPC operations of the suite that cannot be served, each
// with the reason.
func (i *Index) Problems() []Problem { return append([]Problem(nil), i.problems...) }

func (i *Index) lookup(fullMethod string) (method, bool) {
	entry, ok := i.methods[fullMethod]
	return entry, ok
}

// services lists the fully qualified names of the services the suite mocks.
func (i *Index) services() []string {
	seen := map[string]struct{}{}
	for _, entry := range i.methods {
		seen[string(entry.desc.Parent().FullName())] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Check compiles the suite's protos and reports the gRPC operations that cannot
// be served, for the suite's mock step to warn about.
func Check(suite *suites.Definition) []Problem {
	return Build(suite).Problems()
}

// Build compiles the suite's .proto files and matches its gRPC operations to
// their methods. One file that does not compile does not stop the others.
func Build(suite *suites.Definition) *Index {
	index := &Index{methods: map[string]method{}, byPath: map[string]protoreflect.FileDescriptor{}}
	if suite == nil {
		return index
	}

	sources := protoSources(suite)
	paths := make([]string, 0, len(sources))
	for filePath := range sources {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)

	compiler := protocompile.Compiler{
		Resolver: protocompile.WithStandardImports(&protocompile.SourceResolver{
			Accessor: protocompile.SourceAccessorFromMap(importable(sources)),
		}),
	}
	failed := map[string]string{} // file -> compiler message
	for _, filePath := range paths {
		compiled, err := compiler.Compile(context.Background(), filePath)
		if err != nil {
			failed[filePath] = firstLine(err.Error())
			continue
		}
		for _, file := range compiled {
			index.files = append(index.files, file)
			index.byPath[file.Path()] = file
		}
	}

	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			if !isGRPC(surface, operation) {
				continue
			}
			found, problem := resolveOperation(operation, index, paths, failed)
			if problem != "" {
				index.problems = append(index.problems, Problem{Surface: surface.ID, Operation: operation.ID, Reason: problem})
				continue
			}
			index.methods["/"+string(found.Parent().FullName())+"/"+string(found.Name())] = method{surface: surface.ID, operation: operation.ID, desc: found}
		}
	}
	return index
}

// isGRPC reports whether the operation is a gRPC mock: its own adapter says so,
// or, when it names none, its surface's protocol does.
func isGRPC(surface suites.APISurface, operation suites.APIOperation) bool {
	if adapter := strings.TrimSpace(operation.MockMetadata.Adapter); adapter != "" {
		return strings.EqualFold(adapter, "grpc")
	}
	return strings.EqualFold(strings.TrimSpace(surface.Protocol), "grpc")
}

// resolveOperation finds the method a gRPC operation mocks: the one its contract
// path names (api/proto/x.proto#Method), or else the method whose name is the
// operation's id in PascalCase (quote-refund -> QuoteRefund).
func resolveOperation(operation suites.APIOperation, index *Index, protoPaths []string, failed map[string]string) (protoreflect.MethodDescriptor, string) {
	file, rpc, _ := strings.Cut(strings.TrimSpace(operation.ContractPath), "#")
	file, rpc = strings.Trim(strings.TrimSpace(file), "/"), strings.TrimSpace(rpc)
	if rpc == "" {
		rpc = pascalCase(operation.ID)
	}

	candidates := protoPaths
	if file != "" {
		candidates = nil
		for _, candidate := range protoPaths {
			if candidate == file || strings.HasSuffix(candidate, "/"+file) {
				candidates = append(candidates, candidate)
			}
		}
		if len(candidates) == 0 {
			return nil, fmt.Sprintf("its contract %s is not among the suite's files", file)
		}
	}
	if len(candidates) == 0 {
		return nil, "the suite has no .proto file"
	}

	for _, candidate := range candidates {
		descriptor, ok := index.byPath[candidate]
		if !ok {
			continue
		}
		services := descriptor.Services()
		for s := 0; s < services.Len(); s++ {
			if found := services.Get(s).Methods().ByName(protoreflect.Name(rpc)); found != nil {
				return found, ""
			}
		}
	}
	for _, candidate := range candidates {
		if message, bad := failed[candidate]; bad {
			return nil, fmt.Sprintf("%s does not compile: %s", candidate, message)
		}
	}
	return nil, fmt.Sprintf("no service in %s declares an rpc named %s", strings.Join(candidates, ", "), rpc)
}

// protoSources returns the suite's .proto files by path. Files from the suite's
// package and its seed sources are both read: a suite pulled from a registry
// keeps them in one, a built-in one in the other.
func protoSources(suite *suites.Definition) map[string]string {
	out := map[string]string{}
	for _, group := range [][]suites.SourceFile{suite.SeedSources, suite.SourceFiles} {
		for _, file := range group {
			name := strings.Trim(strings.TrimSpace(file.Path), "/")
			if strings.EqualFold(path.Ext(name), ".proto") && file.Content != "" {
				out[name] = file.Content
			}
		}
	}
	return out
}

// importable lets one proto import another by the name the author wrote: relative
// to the suite, to api/proto, or by file name alone.
func importable(sources map[string]string) map[string]string {
	out := make(map[string]string, len(sources)*2)
	for filePath, content := range sources {
		out[filePath] = content
		for _, alias := range []string{path.Base(filePath), strings.TrimPrefix(filePath, "api/proto/"), strings.TrimPrefix(filePath, "api/")} {
			if _, taken := out[alias]; !taken {
				out[alias] = content
			}
		}
	}
	return out
}

// digest identifies the protos and operations an Index was built from.
func digest(suite *suites.Definition) string {
	sum := sha256.New()
	sources := protoSources(suite)
	paths := make([]string, 0, len(sources))
	for filePath := range sources {
		paths = append(paths, filePath)
	}
	sort.Strings(paths)
	for _, filePath := range paths {
		fmt.Fprintf(sum, "%s\x00%d\x00%s\x00", filePath, len(sources[filePath]), sources[filePath])
	}
	for _, surface := range suite.APISurfaces {
		for _, operation := range surface.Operations {
			fmt.Fprintf(sum, "op\x00%s\x00%s\x00%s\x00%s\x00", surface.ID, operation.ID, operation.ContractPath, operation.MockMetadata.Adapter)
		}
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func pascalCase(id string) string {
	var out strings.Builder
	upper := true
	for _, r := range id {
		switch {
		case r == '-' || r == '_' || r == ' ' || r == '.':
			upper = true
		case upper:
			out.WriteRune(unicode.ToUpper(r))
			upper = false
		default:
			out.WriteRune(r)
		}
	}
	return out.String()
}

func firstLine(text string) string {
	if cut := strings.IndexAny(text, "\r\n"); cut >= 0 {
		return strings.TrimSpace(text[:cut])
	}
	return strings.TrimSpace(text)
}
