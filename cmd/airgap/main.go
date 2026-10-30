// Command airgap packs, verifies, deploys, and attests AI bundles for
// environments with no internet egress.
//
//	airgap pack    <dir>                     build manifest.json for a bundle
//	airgap verify  <dir>                     check every file against the manifest
//	airgap check                             probe egress; attest air-gap status
//	airgap serve   <dir>                     launch the bundled inference runtime
//	airgap eval    <dir>                     run the bundle eval suite against an endpoint
//	airgap report  <dir>                     write the accreditation markdown report
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/rahmankhanjohn/airgap-ai-stack/internal/bundle"
	"github.com/rahmankhanjohn/airgap-ai-stack/internal/egress"
	"github.com/rahmankhanjohn/airgap-ai-stack/internal/eval"
	"github.com/rahmankhanjohn/airgap-ai-stack/internal/report"
)

const usage = `airgap - AI bundles for environments with no internet egress

Usage (flags before the directory, per Go flag convention):
  airgap pack   [--name N] [--runtime BIN] [--port P] [--eval-suite PATH] <dir>
  airgap verify [--json] <dir>
  airgap check  [--timeout 5s] [--json]
  airgap serve  [--port P] [--runtime PATH] [--model FILE] <dir>
  airgap eval   [--endpoint URL] [--port P] [--json] <dir>
  airgap report [--endpoint URL] [--out FILE] <dir>

Runtime: launch any OpenAI-compatible server (llama-server, ollama) from the
bundle bin/ directory or PATH. The binary itself makes no network calls except
the probes you ask for (check) and the endpoint you point eval at.`

func main() {
	if len(os.Args) < 2 {
		fmt.Println(usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "pack":
		err = cmdPack(os.Args[2:])
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "check":
		err = cmdCheck(os.Args[2:])
	case "serve":
		err = cmdServe(os.Args[2:])
	case "eval":
		err = cmdEval(os.Args[2:])
	case "report":
		err = cmdReport(os.Args[2:])
	case "-h", "--help", "help":
		fmt.Println(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", os.Args[1], usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func fs(args []string) *flag.FlagSet {
	name := "airgap"
	if len(args) > 0 {
		name = args[0]
	}
	return flag.NewFlagSet(name, flag.ExitOnError)
}

func cmdPack(args []string) error {
	f := fs(args)
	name := f.String("name", "enclave-bundle", "bundle name")
	runtimeBin := f.String("runtime", "llama-server", "inference runtime binary")
	port := f.Int("port", 8080, "default listen port")
	suite := f.String("eval-suite", "", "path to the eval suite inside the bundle")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return fmt.Errorf("usage: airgap pack <dir>")
	}
	root := f.Arg(0)
	m, err := bundle.Pack(root, *name, bundle.RuntimeSpec{
		Binary:     *runtimeBin,
		ListenPort: *port,
		HealthPath: "/health",
	}, *suite)
	if err != nil {
		return err
	}
	fmt.Printf("packed %d files into %s/manifest.json\n", len(m.Files), root)
	for _, fe := range m.Files {
		fmt.Printf("  %-8s %-40s %8d bytes\n", fe.Role, fe.Path, fe.Bytes)
	}
	return nil
}

func cmdVerify(args []string) error {
	f := fs(args)
	jsonOut := f.Bool("json", false, "machine-readable output")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return fmt.Errorf("usage: airgap verify <dir>")
	}
	root := f.Arg(0)
	m, problems, err := bundle.Verify(root)
	if err != nil {
		return err
	}
	if *jsonOut {
		out := struct {
			Files    int      `json:"files"`
			Problems []string `json:"problems"`
			Pass     bool     `json:"pass"`
		}{len(m.Files), problems, len(problems) == 0}
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	fmt.Printf("bundle %s: %d files\n", m.Name, len(m.Files))
	if len(problems) == 0 {
		fmt.Println("verify: PASS - every file present, every hash matches")
		return nil
	}
	fmt.Println("verify: FAIL")
	for _, p := range problems {
		fmt.Println("  " + p)
	}
	return fmt.Errorf("%d verification problems", len(problems))
}

func cmdCheck(args []string) error {
	f := fs(args)
	timeout := f.Duration("timeout", 6*time.Second, "per-probe budget")
	jsonOut := f.Bool("json", false, "machine-readable output")
	if err := f.Parse(args); err != nil {
		return err
	}
	sum := egress.Check(*timeout)
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(sum)
	}
	fmt.Printf("egress check at %s\n", sum.At.Format(time.RFC3339))
	for _, p := range sum.Probes {
		state := "UNREACHABLE"
		if p.OK {
			state = "REACHABLE"
		}
		detail := p.Error
		if detail == "" {
			detail = p.Latency
		}
		fmt.Printf("  %-8s %-24s %-12s %s\n", p.Probe, p.Target, state, detail)
	}
	fmt.Println("verdict:", sum.Verdict)
	if !sum.AirGap {
		return fmt.Errorf("egress detected: this host is not air-gapped")
	}
	return nil
}

func cmdServe(args []string) error {
	f := fs(args)
	port := f.Int("port", 8080, "listen port")
	runtimePath := f.String("runtime", "", "path to runtime binary (default: bundle manifest / PATH)")
	model := f.String("model", "", "model file override (default: first model in manifest)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return fmt.Errorf("usage: airgap serve <dir>")
	}
	root := f.Arg(0)
	m, problems, err := bundle.Verify(root)
	if err != nil {
		return err
	}
	if len(problems) > 0 {
		return fmt.Errorf("bundle failed verification (%d problems); refusing to serve", len(problems))
	}
	bin := *runtimePath
	if bin == "" {
		bin = m.Runtime.Binary
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		// also try bundle bin/ directory
		cand := root + "/bin/" + bin
		if _, statErr := os.Stat(cand); statErr == nil {
			path, err = cand, nil
		}
		if err != nil {
			return fmt.Errorf("runtime %q not found in PATH or %s/bin: %w", bin, root, err)
		}
	}
	modelFile := *model
	if modelFile == "" {
		for _, fe := range m.Files {
			if fe.Role == "model" {
				modelFile = root + "/" + fe.Path
				break
			}
		}
	}
	if modelFile == "" {
		return fmt.Errorf("bundle has no model file; pass --model")
	}
	listen := fmt.Sprintf("127.0.0.1:%d", *port)
	cmd := exec.Command(path, "-m", modelFile, "--host", "127.0.0.1", "--port", fmt.Sprint(*port))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start runtime: %w", err)
	}
	fmt.Printf("runtime pid %d, model %s, listening on http://%s\n", cmd.Process.Pid, modelFile, listen)

	health := "http://" + listen + "/health"
	for i := 0; i < 60; i++ {
		resp, err := http.Get(health)
		if err == nil {
			resp.Body.Close()
			fmt.Println("runtime healthy:", health)
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	fmt.Println("shutting down runtime")
	cmd.Process.Signal(os.Interrupt)
	done := make(chan struct{})
	go func() { cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
	}
	return nil
}

func cmdEval(args []string) error {
	f := fs(args)
	endpoint := f.String("endpoint", "", "OpenAI-compatible endpoint (default: http://127.0.0.1:<port>)")
	port := f.Int("port", 8080, "default port when --endpoint is not given")
	jsonOut := f.Bool("json", false, "machine-readable output")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return fmt.Errorf("usage: airgap eval <dir>")
	}
	root := f.Arg(0)
	m, _, err := bundle.Verify(root)
	if err != nil {
		return err
	}
	suitePath := m.EvalSuite
	if suitePath == "" {
		// first eval file in the bundle
		for _, fe := range m.Files {
			if fe.Role == "eval" {
				suitePath = fe.Path
				break
			}
		}
	}
	if suitePath == "" {
		return fmt.Errorf("bundle has no eval suite; pass --eval-suite at pack time")
	}
	ep := *endpoint
	if ep == "" {
		p := m.Runtime.ListenPort
		if p == 0 {
			p = *port
		}
		ep = fmt.Sprintf("http://127.0.0.1:%d", p)
	}
	suite, err := eval.LoadSuite(root + "/" + suitePath)
	if err != nil {
		return err
	}
	rep, err := eval.Run(suite, ep, "", 120*time.Second)
	if err != nil {
		return err
	}
	if *jsonOut {
		return json.NewEncoder(os.Stdout).Encode(rep)
	}
	fmt.Printf("eval suite %s against %s\n", rep.Suite, rep.Endpoint)
	for _, r := range rep.Results {
		verdict := "PASS"
		detail := ""
		if !r.Passed {
			verdict = "FAIL"
			detail = fmt.Sprintf("%v", r.Failures)
		}
		if r.Error != "" {
			verdict = "ERROR"
			detail = r.Error
		}
		fmt.Printf("  %-28s %-6s %8s %s\n", r.ID, verdict, r.Duration, detail)
	}
	fmt.Printf("score: %d/%d (%.0f%%)\n", rep.Passed, rep.Total, rep.Score*100)
	if rep.Failed > 0 || rep.Passed == 0 {
		return fmt.Errorf("eval suite failed")
	}
	return nil
}

func cmdReport(args []string) error {
	f := fs(args)
	endpoint := f.String("endpoint", "", "eval endpoint (optional: skips eval section)")
	out := f.String("out", "", "output file (default: stdout)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return fmt.Errorf("usage: airgap report <dir>")
	}
	root := f.Arg(0)
	m, problems, err := bundle.Verify(root)
	if err != nil {
		return err
	}
	in := report.Input{
		Bundle:    m,
		BundleDir: root,
		Problems:  problems,
		Host:      runtime.GOOS + "/" + runtime.GOARCH,
	}
	if *endpoint != "" {
		if m.EvalSuite == "" {
			for _, fe := range m.Files {
				if fe.Role == "eval" {
					m.EvalSuite = fe.Path
					break
				}
			}
		}
		if m.EvalSuite != "" {
			if suite, err := eval.LoadSuite(root + "/" + m.EvalSuite); err == nil {
				if rep, err := eval.Run(suite, *endpoint, "", 120*time.Second); err == nil {
					in.Eval = rep
				} else {
					in.Notes = fmt.Sprintf("eval skipped: %v\n", err)
				}
			}
		}
	}
	in.Egress = ptr(egress.Check(6 * time.Second))
	md := report.Markdown(in)
	if *out == "" {
		fmt.Print(md)
		return nil
	}
	if err := os.WriteFile(*out, []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Println("report written:", *out)
	return nil
}

func ptr[T any](v T) *T { return &v }
