# airgap-ai-stack

![Tests](https://github.com/rahmankhanjohn/airgap-ai-stack/actions/workflows/test.yml/badge.svg)
![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)
![XERJ Corpus Hub contributor](https://hub.xerj.org/contributors/rahmankhanjohn/badge.svg)
![Setup manual](https://gitmanual.com/rahmankhanjohn/airgap-ai-stack/badge.svg)
![Go 1.24+](https://img.shields.io/badge/go-1.24+-00ADD8.svg)
![Dependencies: 0](https://img.shields.io/badge/dependencies-0-brightgreen.svg)
![Tests: 17](https://img.shields.io/badge/tests-17%20passing-brightgreen.svg)
![GitHub stars](https://img.shields.io/github/stars/rahmankhanjohn/airgap-ai-stack?style=social)
![Last commit](https://img.shields.io/github/last-commit/rahmankhanjohn/airgap-ai-stack)


Run an open-weight AI model inside an enclave with no internet egress - and prove it.

One Go binary packs models, eval suites, and runtime binaries into a checksummed
bundle, verifies integrity on the far side of the air gap, launches any
OpenAI-compatible runtime (llama-server, ollama), runs deterministic evals against
the local endpoint, and generates the accreditation report an auditor asks for.

Part of the "AI that runs where the internet does not reach" thesis:
[air-gapped AI](https://www.ycombinator.com/rfs#ai-for-the-air-gapped-economy) and
[owned intelligence](https://www.ycombinator.com/rfs#own-your-intelligence).

## Why

Defense contractors, banks, hospitals, and industrial sites keep their data behind
walls. Downloading an open model is the easy part. The hard part is what this tool
does: proving the bundle is intact, proving the enclave has no egress, proving the
model still makes the right operational decisions after deployment. Those proofs
are the accreditation.

## Quick start

```bash
# On an internet machine: bring a GGUF model and build a bundle
airgap pack --name enclave-x --runtime llama-server --eval-suite evals/suite.json ./bundle

# Sneakernet the bundle directory across the gap, then inside the enclave:
airgap verify ./bundle                  # every hash must match
airgap check                            # attest: no egress detected
airgap serve ./bundle --port 8080       # launch llama-server from bundle bin/ or PATH
airgap eval ./bundle --endpoint http://127.0.0.1:8080
airgap report ./bundle --endpoint http://127.0.0.1:8080 --out accreditation.md
```

Flags go before the directory (Go flag convention).

## Commands

| Command | What it does | Exit codes |
|---------|--------------|------------|
| `pack` | Hash every file in a bundle directory, write `manifest.json`. Model files must be GGUF. | 0 ok, 1 error, 2 usage |
| `verify` | Check presence, size, SHA256 of every manifest entry; flag unlisted files. | 0 pass, 1 fail |
| `check` | Probe DNS and TCP egress. Report: no egress / partial / detected. The attestation probe. | 0 air-gapped, 1 egress found |
| `serve` | Verify bundle, launch the runtime binary with offline-safe flags, wait for `/health`. | 0 on clean shutdown |
| `eval` | Run the bundle's eval suite against the local endpoint. Deterministic checks: contains, contains_any, not_contains, regex, exact. No judge model, no network. | 0 all pass, 1 fail |
| `report` | Assemble the accreditation markdown: bundle contents, checksums, egress attestation, eval results, host identity. | 0 written |

## Bundle layout

```
bundle/
├── manifest.json          # written by `airgap pack`
├── models/*.gguf          # GGUF models (magic bytes checked)
├── evals/suite.json       # deterministic eval suite
├── bin/                   # optional: runtime binary (llama-server)
└── docs/                  # anything the enclave team needs
```

See [examples/bundle](examples/bundle) for a working sample with the
`failover-diagnosis` eval suite distilled from the
[failover-budget](https://github.com/rahmankhanjohn/failover-budget) operator runbook.

## Design rules

1. **No third-party dependencies.** Go standard library only. Eval suites are JSON.
2. **Single binary.** `airgap` is the whole tool.
3. **The binary makes no network calls** except the probes you explicitly run
   (`check`) and the endpoint you explicitly point `eval` at. Serving binds 127.0.0.1.
4. **Verify before serve.** `serve` refuses to launch a bundle that fails verification.
5. **Deterministic evals only.** Scoring is exact; temperature is pinned to 0.

## Security model

- `check` is an attestation helper, not a guarantee: it probes a fixed target list
  from this host only. Scope and limits in [SECURITY.md](SECURITY.md).
- Model files are validated by GGUF magic bytes and SHA256; integrity of the runtime
  binary is covered by the manifest hash of `bin/`.

## Recognition

Indexed on the [XERJ Corpus Hub](https://hub.xerj.org/contributors/rahmankhanjohn) - the open reference memory AI coding agents search before they write code.

## Requirements

Go 1.24+ to build. The runtime (llama-server or ollama) is whatever your enclave
approved - `airgap` launches it, it does not bundle one.

## License

MIT

## Who is this for?

No telemetry in this tool. If you use it, plan to, or rejected it, tell us your environment in [the discussion thread](https://github.com/rahmankhanjohn/airgap-ai-stack/discussions/1). Operator replies steer the roadmap.
