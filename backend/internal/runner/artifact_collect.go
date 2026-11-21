package runner

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	// maxArtifactBytes is the most one artifact file may hold. A larger file is
	// reported as truncated and not read, so nothing parses half a document.
	maxArtifactBytes = 10 * 1024 * 1024
	// maxArtifactFiles is how many files one glob export collects.
	maxArtifactFiles = 50
	// maxStepArtifactBytes is the most a step's exports may hold in all.
	maxStepArtifactBytes = 50 * 1024 * 1024
)

// ArtifactFile is one file a step's export matched.
type ArtifactFile struct {
	Path    string
	Content []byte
	// Truncated is set when the file, or the step's budget, was too large to
	// read. Content is then empty.
	Truncated bool
}

// ArtifactResult is what a backend found for one declared export after the
// step's container finished. Every declared export is reported, whether or not
// its on= trigger matches: the control plane decides what to keep once it knows
// the step's final status, which a log check can change after the container
// has exited.
type ArtifactResult struct {
	// Export is the path or glob as the suite declared it.
	Export string
	// Files are the matched files in lexical order. Empty when Missing, Err or
	// NotCollected is set.
	Files []ArtifactFile
	// Missing is set when no file matched.
	Missing bool
	// Err is a read error other than a file that does not exist.
	Err error
	// NotCollected is set by a backend that cannot read files from where the
	// step ran, and says why.
	NotCollected string
}

// collectMountArtifacts reads every declared export from the host-side mount of
// the step's workspace. A path is cleaned and checked to stay inside the mount;
// glob patterns (*, ?, [) match every file, up to the limits above.
func collectMountArtifacts(mountDir string, exports []ArtifactExport) []ArtifactResult {
	results := make([]ArtifactResult, 0, len(exports))
	budget := int64(maxStepArtifactBytes)
	for _, export := range exports {
		results = append(results, collectMountArtifact(mountDir, strings.TrimSpace(export.Path), &budget))
	}
	return results
}

func collectMountArtifact(mountDir, exportPath string, budget *int64) ArtifactResult {
	result := ArtifactResult{Export: exportPath}

	pattern := filepath.Join(mountDir, filepath.FromSlash(path.Clean("/"+exportPath)))
	prefix := filepath.Clean(mountDir) + string(filepath.Separator)
	if !strings.HasPrefix(pattern+string(filepath.Separator), prefix) {
		result.Err = fmt.Errorf("artifact path %q escapes the workspace", exportPath)
		return result
	}

	matches := []string{pattern}
	if strings.ContainsAny(exportPath, "*?[") {
		found, err := filepath.Glob(pattern)
		if err != nil {
			result.Err = fmt.Errorf("artifact glob %q: %w", exportPath, err)
			return result
		}
		matches = found
	}

	for _, match := range matches {
		if !strings.HasPrefix(match+string(filepath.Separator), prefix) {
			result.Err = fmt.Errorf("artifact glob match %q escapes the workspace", match)
			return result
		}
		info, err := os.Stat(match)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			result.Err = err
			return result
		}
		if info.IsDir() {
			if !strings.ContainsAny(exportPath, "*?[") {
				result.Err = fmt.Errorf("artifact path %q is a directory, not a file", exportPath)
				return result
			}
			continue
		}

		name := strings.TrimPrefix(filepath.ToSlash(strings.TrimPrefix(match, prefix)), "/")
		if len(result.Files) >= maxArtifactFiles {
			result.Files = append(result.Files, ArtifactFile{Path: name, Truncated: true})
			continue
		}
		content, truncated, err := readArtifactFile(match, budget)
		if err != nil {
			result.Err = err
			result.Files = nil
			return result
		}
		result.Files = append(result.Files, ArtifactFile{Path: name, Content: content, Truncated: truncated})
	}

	if len(result.Files) == 0 {
		result.Missing = true
	}
	return result
}

// readArtifactFile reads one file within the per-file limit and what is left of
// the step's budget. A file over either is reported truncated and not read.
func readArtifactFile(file string, budget *int64) ([]byte, bool, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, false, err
	}
	defer f.Close()

	limit := int64(maxArtifactBytes)
	if *budget < limit {
		limit = *budget
	}
	content, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(content)) > limit {
		return nil, true, nil
	}
	*budget -= int64(len(content))
	return content, false, nil
}

// notCollectedResults reports every declared export as one a backend could not
// read, for backends whose steps run where the control plane cannot see files.
func notCollectedResults(exports []ArtifactExport, reason string) []ArtifactResult {
	results := make([]ArtifactResult, 0, len(exports))
	for _, export := range exports {
		results = append(results, ArtifactResult{Export: strings.TrimSpace(export.Path), NotCollected: reason})
	}
	return results
}

// deliverArtifacts hands each result to the step's OnArtifact callback.
func deliverArtifacts(step StepSpec, results []ArtifactResult) {
	if step.OnArtifact == nil {
		return
	}
	for _, result := range results {
		step.OnArtifact(result)
	}
}
