// Package bundle packs and verifies air-gap bundles: a manifest plus the
// model, eval, and binary files an enclave needs, with SHA256 over every file.
package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Version is the bundle format version this build writes.
const Version = "0.1.0"

// FileEntry describes one file in the bundle.
type FileEntry struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
	Role   string `json:"role"` // model | eval | runtime | doc | other
}

// Manifest is the bundle manifest written as manifest.json at the bundle root.
type Manifest struct {
	Name          string      `json:"name"`
	Created       time.Time   `json:"created"`
	FormatVersion string      `json:"format_version"`
	ToolVersion   string      `json:"tool_version"`
	EvalSuite     string      `json:"eval_suite,omitempty"`
	Runtime       RuntimeSpec `json:"runtime"`
	Files         []FileEntry `json:"files"`
}

// RuntimeSpec names the inference runtime the bundle expects and its settings.
type RuntimeSpec struct {
	Binary      string `json:"binary"`       // e.g. llama-server or ollama
	Args        string `json:"args,omitempty"`
	MinContext  int    `json:"min_context,omitempty"`
	ListenPort  int    `json:"listen_port,omitempty"`
	HealthPath  string `json:"health_path,omitempty"`
}

// ggufMagic is the first 4 bytes of every GGUF model file.
var ggufMagic = []byte("GGUF")

var roleDirs = map[string]string{
	"model":   "models",
	"eval":    "evals",
	"runtime": "bin",
	"doc":     "docs",
}

func roleFor(path string) string {
	dir := strings.SplitN(filepath.ToSlash(path), "/", 2)[0]
	for role, d := range roleDirs {
		if dir == d {
			return role
		}
	}
	return "other"
}

// Pack walks root, hashes every regular file, and writes manifest.json.
// It returns the manifest. Model files must be GGUF; pack fails otherwise.
func Pack(root, name string, runtime RuntimeSpec, evalSuite string) (*Manifest, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("bundle root: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("bundle root %s is not a directory", root)
	}
	m := &Manifest{
		Name:          name,
		Created:       time.Now().UTC(),
		FormatVersion: Version,
		ToolVersion:   Version,
		Runtime:       runtime,
		EvalSuite:     evalSuite,
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		base := filepath.Base(path)
		if base == "manifest.json" || strings.HasPrefix(base, ".") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		sum, size, err := hashFile(path)
		if err != nil {
			return fmt.Errorf("hash %s: %w", rel, err)
		}
		role := roleFor(rel)
		if role == "model" && !isGGUF(path) {
			return fmt.Errorf("model file %s is not GGUF (bad magic)", rel)
		}
		m.Files = append(m.Files, FileEntry{Path: rel, SHA256: sum, Bytes: size, Role: role})
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(m.Files) == 0 {
		return nil, fmt.Errorf("bundle %s contains no files", root)
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0o644); err != nil {
		return nil, err
	}
	return m, nil
}

// Verify checks the manifest against the files on disk: every listed file
// present, every hash matching, no unlisted files except manifest.json.
func Verify(root string) (*Manifest, []string, error) {
	raw, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, nil, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, nil, fmt.Errorf("parse manifest: %w", err)
	}
	var problems []string
	seen := map[string]bool{".": true, "manifest.json": true}
	for _, fe := range m.Files {
		seen[filepath.ToSlash(fe.Path)] = true
		full := filepath.Join(root, filepath.FromSlash(fe.Path))
		sum, size, err := hashFile(full)
		if err != nil {
			problems = append(problems, fmt.Sprintf("MISSING %s", fe.Path))
			continue
		}
		if size != fe.Bytes {
			problems = append(problems, fmt.Sprintf("SIZE %s: manifest %d, disk %d", fe.Path, fe.Bytes, size))
		}
		if sum != fe.SHA256 {
			problems = append(problems, fmt.Sprintf("HASH %s: expected %s, got %s", fe.Path, fe.SHA256[:16]+"...", sum[:16]+"..."))
			continue
		}
		if fe.Role == "model" && !isGGUF(full) {
			problems = append(problems, fmt.Sprintf("MODEL %s: not GGUF", fe.Path))
		}
	}
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if !seen[rel] {
			problems = append(problems, fmt.Sprintf("UNLISTED %s", rel))
		}
		return nil
	})
	if err != nil {
		return nil, problems, err
	}
	return &m, problems, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func isGGUF(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	magic := make([]byte, len(ggufMagic))
	if _, err := io.ReadFull(f, magic); err != nil {
		return false
	}
	return string(magic) == string(ggufMagic)
}
