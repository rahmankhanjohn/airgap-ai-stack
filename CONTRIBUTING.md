# Contributing to airgap-ai-stack

Thanks for taking the time to contribute.

## What this project is

A single Go binary that packs, verifies, deploys, and attests open-weight AI
bundles for environments with no internet egress. Zero third-party dependencies,
MIT licence. The point: an operator gets a provable chain - bundle integrity,
egress attestation, deterministic evals - on a laptop with the network off.

## Getting started

```
git clone https://github.com/rahmankhanjohn/airgap-ai-stack.git
cd airgap-ai-stack
go test ./... -count=1
```

All tests pass on Go 1.24 or newer. No install step. CI runs on Linux; local
development on Windows is fine (Windows Defender may false-positive freshly
compiled test binaries - rerun if unlinkat errors appear).

## Rules of the codebase

1. **No third-party dependencies.** Standard library only. If a feature needs a
   package, it does not go in this repo. Eval suites are JSON, not YAML, for this reason.
2. **Single binary.** `cmd/airgap` is the whole tool. Internal packages stay internal.
3. **Every feature needs a test.** Egress probes use an injectable dialer; evals use
   an injectable HTTP client, so tests never touch the network.
4. **Exit codes matter.** 0 = pass, 1 = fail, 2 = usage. Do not break this contract.
5. **The binary makes no network calls** except `check` probes and the endpoint
   `eval` is pointed at. Any new outbound call is a design change - open an issue first.
6. **Plain English in output.** Every verdict carries a plain sentence. No jargon without a gloss.
7. **No em dashes.** Use periods or commas.

## How to contribute

1. Fork the repository.
2. Create a branch: `git checkout -b feat/your-change`.
3. Make your change. Add or update tests.
4. Run: `go vet ./... && go test ./... -count=1`.
5. Open a pull request with a short description of what you changed and why.

## What we are looking for

- Additional deterministic check types (JSON schema, length bounds, refusal checks)
- Runtime integrations behind the same exec+health pattern
- Report formats (PDF via markdown pipeline, SARIF for findings)
- Documentation improvements
- Bug reports with reproduction steps

## What we are not looking for

- Third-party dependencies
- Bundling a model or runtime into the repo
- Cloud features. This tool exists for hosts with no cloud.

## Reporting bugs

Open a GitHub issue with:
- The command you ran
- The output you got
- The output you expected
- Your Go version and operating system

## Code of conduct

Be direct, be specific, be kind. This is a tool for operators working inside
constraints most of the industry never sees. Don't make them work harder to use it.
