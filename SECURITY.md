# Security Policy

## Supported versions

Only the latest release on `main` is supported. Older releases receive no security fixes.

## Reporting a vulnerability

Do not open a public issue for security reports.

Email rahman.khan.john@gmail.com with:
- A description of the vulnerability
- Steps to reproduce
- The affected version or commit
- Any known workarounds

You will receive a response within 72 hours. If the report is confirmed, a fix will be
released and you will be credited (unless you prefer otherwise).

## Threat model and scope

`airgap` is a local CLI that reads bundle files you point it at and launches a runtime
binary you provide. It is designed for hosts with no internet egress.

Network behaviour, exhaustively:
- `pack`, `verify`, `report` (without --endpoint): zero network calls. File I/O only.
- `check`: outbound DNS lookups and TCP dials to the fixed probe list (one.one.one.one,
  1.1.1.1:443, 8.8.8.8:443, example.com:80). This is the attestation feature.
- `serve`: binds 127.0.0.1 only. Launches your runtime binary as a child process.
- `eval` / `report --endpoint`: HTTP POST to the endpoint you explicitly pass.

Out of scope:
- Vulnerabilities in the Go runtime, the runtime binary you launch (llama-server, ollama),
  or the models in your bundle
- Bypassing the enclave's own network controls: `check` reports what it observes from this
  host with its fixed probe list. It is an attestation aid for auditors, not a guarantee
  that no covert channel exists
- Running the tool on untrusted bundles without reading what is in them

## What this tool does not do

- It does not harden the host, sandbox the runtime, or manage model licences
- It does not encrypt bundles in transit; sneakernet handling of the bundle directory is
  the operator's procedure

## Disclosure policy

Coordinated disclosure. The fix ships first, the advisory follows within 30 days.
