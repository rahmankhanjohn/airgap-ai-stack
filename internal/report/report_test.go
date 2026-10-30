package report

import (
	"strings"
	"testing"

	"github.com/rahmankhanjohn/airgap-ai-stack/internal/bundle"
	"github.com/rahmankhanjohn/airgap-ai-stack/internal/egress"
	"github.com/rahmankhanjohn/airgap-ai-stack/internal/eval"
)

func TestMarkdownFullReport(t *testing.T) {
	m := &bundle.Manifest{
		Name:          "enclave-x",
		FormatVersion: "0.1.0",
		Runtime:       bundle.RuntimeSpec{Binary: "llama-server", ListenPort: 8080},
		Files: []bundle.FileEntry{
			{Path: "models/tiny.gguf", Role: "model", Bytes: 260, SHA256: "abcdef1234567890abcdef1234567890"},
			{Path: "evals/suite.json", Role: "eval", Bytes: 900, SHA256: "11111111111111111111111111111111"},
		},
	}
	in := Input{
		Bundle:    m,
		BundleDir: "/bundles/x",
		Egress:    &egress.Summary{AirGap: true, Verdict: "NO EGRESS DETECTED - air-gap behaviour confirmed on all probes", Probes: []egress.Result{{Probe: "dns", Target: "one.one.one.one", Error: "no such host"}}},
		Eval:      &eval.Report{Suite: "failover-diagnosis", Endpoint: "http://127.0.0.1:8080", Passed: 3, Failed: 1, Total: 4, Score: 0.75, Results: []eval.CaseResult{{ID: "ttl-term", Passed: true, Duration: "120ms"}, {ID: "rollback", Passed: false, Failures: []string{"contains \"rollback\""}}}},
		Host:      "linux/amd64",
	}
	md := Markdown(in)
	for _, want := range []string{
		"# Air-Gap Accreditation Report", "enclave-x", "models/tiny.gguf", "abcdef1234567890",
		"Verdict: **PASS**", "NO EGRESS", "Score: 3/4 (75%)", "linux/amd64", "FAIL", "airgap",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("report missing %q\n---\n%s", want, md)
		}
	}
}

func TestMarkdownEmptyInput(t *testing.T) {
	md := Markdown(Input{Host: "windows/amd64"})
	if !strings.Contains(md, "Air-Gap Accreditation Report") || !strings.Contains(md, "windows/amd64") {
		t.Fatalf("empty report should still render host: %s", md)
	}
}

func TestMarkdownVerificationProblems(t *testing.T) {
	m := &bundle.Manifest{Name: "x", Files: []bundle.FileEntry{{Path: "models/g.gguf", Role: "model"}}}
	md := Markdown(Input{Bundle: m, Problems: []string{"MISSING models/g.gguf"}})
	if !strings.Contains(md, "MISSING models/g.gguf") || !strings.Contains(md, "Verification problems") {
		t.Fatalf("problems should be listed: %s", md)
	}
}
