package bundle

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGGUF(t *testing.T, path string, size int) {
	t.Helper()
	magic := []byte("GGUF")
	body := make([]byte, size)
	if err := os.WriteFile(path, append(magic, body...), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPackAndVerifyRoundTrip(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"models", "evals", "docs"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	writeGGUF(t, filepath.Join(root, "models", "tiny.gguf"), 64)
	if err := os.WriteFile(filepath.Join(root, "evals", "suite.json"), []byte(`{"name":"s","cases":[{"id":"c1","prompt":"p","checks":[{"type":"contains","value":"x"}]}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "placeholder.txt"), []byte("note"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Pack(root, "test-bundle", RuntimeSpec{Binary: "llama-server", ListenPort: 8080, HealthPath: "/health"}, "evals/suite.json")
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	if len(m.Files) != 3 {
		t.Fatalf("expected 3 files, got %d", len(m.Files))
	}
	if _, err := os.Stat(filepath.Join(root, "manifest.json")); err != nil {
		t.Fatal("manifest.json not written")
	}

	m2, problems, err := Verify(root)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got %v", problems)
	}
	if m2.Name != "test-bundle" {
		t.Fatalf("name mismatch: %s", m2.Name)
	}
}

func TestVerifyDetectsTamperedFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0o755)
	writeGGUF(t, filepath.Join(root, "models", "tiny.gguf"), 64)
	if _, err := Pack(root, "b", RuntimeSpec{Binary: "llama-server"}, ""); err != nil {
		t.Fatal(err)
	}
	// tamper after packing
	writeGGUF(t, filepath.Join(root, "models", "tiny.gguf"), 128)
	_, problems, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, "HASH") || strings.HasPrefix(p, "SIZE") {
			found = true
		}
	}
	if !found {
		t.Fatalf("tamper not detected: %v", problems)
	}
}

func TestPackRejectsNonGGUFModel(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0o755)
	os.WriteFile(filepath.Join(root, "models", "fake.gguf"), []byte("this is not a gguf file"), 0o644)
	if _, err := Pack(root, "b", RuntimeSpec{Binary: "llama-server"}, ""); err == nil {
		t.Fatal("expected error for non-GGUF model file")
	}
}

func TestVerifyDetectsUnlistedFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0o755)
	writeGGUF(t, filepath.Join(root, "models", "tiny.gguf"), 64)
	if _, err := Pack(root, "b", RuntimeSpec{Binary: "llama-server"}, ""); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "smuggled.bin"), []byte("sneaky"), 0o644)
	_, problems, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, "UNLISTED") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unlisted file not detected: %v", problems)
	}
}

func TestVerifyDetectsMissingFile(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "models"), 0o755)
	writeGGUF(t, filepath.Join(root, "models", "tiny.gguf"), 64)
	if _, err := Pack(root, "b", RuntimeSpec{Binary: "llama-server"}, ""); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(root, "models", "tiny.gguf"))
	_, problems, err := Verify(root)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range problems {
		if strings.HasPrefix(p, "MISSING") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing file not detected: %v", problems)
	}
}

func TestEmptyBundleRejected(t *testing.T) {
	root := t.TempDir()
	if _, err := Pack(root, "b", RuntimeSpec{Binary: "llama-server"}, ""); err == nil {
		t.Fatal("expected error for empty bundle")
	}
}
