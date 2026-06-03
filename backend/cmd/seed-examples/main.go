package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type suiteMetadata struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		ID    string `yaml:"id"`
		Title string `yaml:"title"`
	} `yaml:"metadata"`
	Spec struct {
		Repository string   `yaml:"repository"`
		Tags       []string `yaml:"tags"`
	} `yaml:"spec"`
}

func main() {
	examplesRoot, err := resolveExamplesRoot(os.Args[1:])
	if err != nil {
		fatalf("error: %v\n", err)
	}

	suitesRoot := filepath.Join(examplesRoot, "oci-suites")
	entries, err := os.ReadDir(suitesRoot)
	if err != nil {
		fatalf("read oci-suites: %v\n", err)
	}

	client := &http.Client{}
	total := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		suiteDir := filepath.Join(suitesRoot, entry.Name())
		metaPath := filepath.Join(suiteDir, "metadata.yaml")
		if _, err := os.Stat(metaPath); err != nil {
			continue
		}

		meta, err := loadMetadata(metaPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: %v\n", entry.Name(), err)
			continue
		}
		if len(meta.Spec.Tags) == 0 {
			fmt.Fprintf(os.Stderr, "skip %s: no tags\n", entry.Name())
			continue
		}

		blob, err := buildTarball(suiteDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: tarball: %v\n", entry.Name(), err)
			continue
		}

		host, repoPath, err := splitRepository(meta.Spec.Repository)
		if err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: repository: %v\n", entry.Name(), err)
			continue
		}
		baseURL := "http://" + host

		if err := pushSuite(client, baseURL, repoPath, meta.Spec.Tags, blob); err != nil {
			fmt.Fprintf(os.Stderr, "skip %s: push: %v\n", entry.Name(), err)
			continue
		}

		fmt.Printf("pushed %s → %s (%s)\n", meta.Metadata.ID, meta.Spec.Repository, strings.Join(meta.Spec.Tags, ", "))
		total++
	}

	fmt.Printf("done: %d suites pushed\n", total)
}

func loadMetadata(path string) (*suiteMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta suiteMetadata
	if err := yaml.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	if meta.Spec.Repository == "" {
		return nil, fmt.Errorf("missing spec.repository")
	}
	return &meta, nil
}

func buildTarball(suiteDir string) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	err := filepath.WalkDir(suiteDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}

		rel, err := filepath.Rel(suiteDir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)


		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		hdr := &tar.Header{
			Name: rel,
			Mode: 0o644,
			Size: int64(len(data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		_, err = tw.Write(data)
		return err
	})
	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func pushSuite(client *http.Client, baseURL, repoPath string, tags []string, layerBlob []byte) error {
	configBlob := []byte(`{"rootfs":{"type":"layers"},"config":{}}`)

	configDigest, err := pushBlob(client, baseURL, repoPath, configBlob, "application/vnd.oci.image.config.v1+json")
	if err != nil {
		return fmt.Errorf("push config: %w", err)
	}

	layerDigest, err := pushBlob(client, baseURL, repoPath, layerBlob, "application/vnd.oci.image.layer.v1.tar+gzip")
	if err != nil {
		return fmt.Errorf("push layer: %w", err)
	}

	manifest := map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    configDigest,
			"size":      len(configBlob),
		},
		"layers": []map[string]any{
			{
				"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
				"digest":    layerDigest,
				"size":      len(layerBlob),
			},
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	for _, tag := range tags {
		target := baseURL + "/v2/" + encodeRepoPath(repoPath) + "/manifests/" + url.PathEscape(tag)
		req, err := http.NewRequest(http.MethodPut, target, bytes.NewReader(manifestJSON))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
			return fmt.Errorf("manifest %s: %s", tag, resp.Status)
		}
	}
	return nil
}

func pushBlob(client *http.Client, baseURL, repoPath string, data []byte, _ string) (string, error) {
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])

	// Check if the blob already exists.
	checkURL := baseURL + "/v2/" + encodeRepoPath(repoPath) + "/blobs/" + digest
	resp, err := client.Head(checkURL)
	if err == nil {
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			return digest, nil
		}
	}

	// Initiate upload.
	startURL := baseURL + "/v2/" + encodeRepoPath(repoPath) + "/blobs/uploads/"
	resp, err = client.Post(startURL, "", nil)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return "", fmt.Errorf("initiate upload: %s", resp.Status)
	}
	location := resp.Header.Get("Location")
	if location == "" {
		return "", fmt.Errorf("no Location header")
	}

	// Resolve relative Location.
	if !strings.HasPrefix(location, "http") {
		location = baseURL + location
	}

	sep := "?"
	if strings.Contains(location, "?") {
		sep = "&"
	}
	putURL := location + sep + "digest=" + url.QueryEscape(digest)

	req, err := http.NewRequest(http.MethodPut, putURL, bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.ContentLength = int64(len(data))

	resp, err = client.Do(req)
	if err != nil {
		return "", err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("upload blob: %s", resp.Status)
	}
	return digest, nil
}

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

func resolveExamplesRoot(args []string) (string, error) {
	if len(args) > 1 {
		return "", fmt.Errorf("usage: seed-examples [examples-root]")
	}
	if len(args) == 1 {
		return filepath.Abs(args[0])
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	// When run from backend/ or repo root, resolve examples/ relative to repo root.
	if filepath.Base(cwd) == "backend" {
		return filepath.Join(filepath.Dir(cwd), "examples"), nil
	}
	return filepath.Join(cwd, "examples"), nil
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format, args...)
	os.Exit(1)
}
