# Security Policy

`sdo-mcp` (module `github.com/scrape-do/sdo-mcp`) is the standalone Scrape.do
**Model Context Protocol (MCP) server**. It speaks newline-framed JSON-RPC 2.0
over stdio and exposes the Scrape.do scraping surface to an LLM, reusing the
[sdo Go SDK](https://github.com/scrape-do/sdo-go) underneath. It is a
token-bearing process: it holds your Scrape.do API token and attaches it to
every outbound request on the model's behalf. The token is the sensitive asset
this server handles, and — because the server sits between an LLM and the
network — so is the boundary between **untrusted third-party data** (scraped
pages, gateway/target error text) and the server's own voice. We treat any
issue that can leak the token, return it to the model, confuse that
data/instruction boundary, or weaken either protection as a security
vulnerability — not just classic memory/parsing bugs.

This document explains how to report a vulnerability privately, what we support,
and how we coordinate disclosure.

## Reporting a Vulnerability

**Do not open a public GitHub issue, pull request, or discussion for a security
problem.** Public disclosure before a fix is available puts every user's token
and traffic at risk.

Report privately through either channel — both reach the maintainer directly:

1. **GitHub Private Vulnerability Reporting (preferred).**
   Go to the repository's **Security** tab → **Report a vulnerability**. This
   opens a private advisory draft visible only to you and the maintainers, keeps
   the full report history in one place, and is the channel we use to issue the
   GHSA advisory and request a CVE.

2. **Email:** `security@scrape.do`
   Use this if you cannot use GitHub's private reporting, or for anything
   sensitive enough that you'd rather not file even a private GitHub draft. PGP is
   available on request — email first and we'll exchange a key before you send
   details.

A good report includes:

- The server version (the `version.Version` constant, or `go list -m
  github.com/scrape-do/sdo-mcp` in a consuming module) and your Go toolchain
  version (`go version`).
- A description of the issue and its impact — especially whether it can expose,
  log, transmit, or **return to the model** the API token, alter where requests
  are sent, or cause scraped/third-party content to be treated as instructions
  rather than data.
- A minimal reproduction: the exact JSON-RPC request(s) sent on stdin (the
  `initialize` / `tools/call` payloads), the server flags/environment it was
  started with, and observed vs. expected behavior on stdout. **Redact your real
  token** — use a placeholder.
- Any known workaround.

Please report responsibly: do not run automated scans against Scrape.do
production endpoints as part of your research, and do not include real customer
tokens or scraped data in your report.

## Scope

In scope:

- **Token leakage to the model.** The API token is the sole credential and must
  **never** be returned in a tool result, a tool error, server diagnostics, a
  panic/stack trace, or any text that reaches the LLM. Every result is passed
  through the SDK's redaction before it leaves the process; any path that
  surfaces the token (or a request URL carrying it) to the model or to logs is
  in scope.
- **Token precedence flaws.** The documented resolution order is
  **`--token` flag → `--token-file` → `SDO_TOKEN` env var**; any bug that lets a lower-priority or
  attacker-controlled source override a higher-priority token is in scope.
- **Data/instruction boundary bypass.** Scraped pages, plugin output, and
  gateway/target error text are untrusted third-party data. They are fenced with
  nonce-delimited markers and must not be able to close that fence early, inject
  a forged marker, or otherwise escape into the server's own voice — nor smuggle
  control characters past the truncation/redaction stage. Anything that breaks
  this fence is in scope.
- **Request integrity:** anything that redirects, downgrades, or silently
  rewrites the destination of a request, mis-escapes the assembled query string,
  or sends the token to an unintended host — including `--insecure-http` being
  honored where it should not be.
- **Input handling:** malformed JSON-RPC framing, schema-validation bypass
  (arguments the server did not advertise reaching the SDK), or resource
  exhaustion from an unbounded response (the `--max-response-bytes` ceiling and
  the `--client-timeout` per-attempt limit), an oversized request line (16 MiB
  limit) or a flood of tool calls (at most 4 run and 64 may be pending).
- **Vulnerable or malicious transitive dependencies** — notably
  `github.com/scrape-do/sdo-go`, `github.com/andybalholm/brotli`, and
  `golang.org/x/text` — that are exploitable through normal server usage.

Out of scope:

- Vulnerabilities in the Scrape.do API service itself — report those through
  Scrape.do's main responsible-disclosure channel, not this repo.
- Vulnerabilities in the underlying SDK (`sdo-go`) that are not reachable
  through this server — report those against the
  [`sdo-go`](https://github.com/scrape-do/sdo-go) repository.
- Issues that require a pre-compromised local machine, a malicious local
  environment the attacker already controls (the operator who starts the server
  already holds the token and controls its stdin), or physical access.
- Prompt-injection effects that stay within the fenced untrusted block and do
  not cross the data/instruction boundary — a model choosing to act on fenced
  data it was told to treat as data is a client/model-policy concern, not a
  server fence failure.
- Findings against internal build/spec tooling that is not distributed in this
  module.

## Supply-Chain Integrity

- `sdo-mcp` is built on the `sdo-go` SDK, which is tested against a cross-language conformance corpus shared by every Scrape.do SDK. Development is **TDD-first**: changes ship with tests, and CI enforces formatting, `go vet`, the race detector, a coverage floor, secret scanning, `govulncheck`, OSV scanning and a drift check against the copy of the server that ships inside the sdo CLI.
- Dependencies and the SHA-pinned GitHub Actions are kept current by Dependabot, with a 7-day cooldown before a new release is proposed.
- Releases are built from tagged commits on `main` by GoReleaser, run from every archive on a runner of its platform, and published only after approval in a protected `release` environment. For version X.Y.Z (the tag `vX.Y.Z`) a release holds:
  - `sdo-mcp_X.Y.Z_<os>_<arch>.tar.gz` for `linux` and `darwin`, and `sdo-mcp_X.Y.Z_windows_<arch>.zip`, each for `amd64` and `arm64` (e.g. `sdo-mcp_X.Y.Z_darwin_arm64.tar.gz`). Each archive holds the `sdo-mcp` binary (`sdo-mcp.exe` on Windows), `LICENSE`, `NOTICE`, `README.md`, `CHANGELOG.md` and `THIRD_PARTY_LICENSES.txt`, the license and notice texts of the Go runtime and every module linked into the binary.
  - `<archive>.sbom.json`, an SPDX SBOM per archive (e.g. `sdo-mcp_X.Y.Z_linux_amd64.tar.gz.sbom.json`).
  - `checksums.txt`, the SHA-256 of every file above, and `checksums.txt.cosign.bundle`, a cosign keyless signature for it, signed by the release workflow's GitHub OIDC identity and recorded in the Rekor transparency log.

  To verify a download (cosign v3):

  ```bash
  cosign verify-blob checksums.txt \
    --bundle checksums.txt.cosign.bundle \
    --certificate-identity "https://github.com/scrape-do/sdo-mcp/.github/workflows/release.yml@refs/tags/vX.Y.Z" \
    --certificate-oidc-issuer https://token.actions.githubusercontent.com
  sha256sum --ignore-missing -c checksums.txt
  ```

- Sibling repositories under the same governance: `sdo-cli` (the CLI, which bundles this server), `sdo-sdk-python` (PyPI `sdo-sdk`), `sdo-sdk-js` (npm `@scrape-do/sdk`), and `sdo-go` (`github.com/scrape-do/sdo-go`).

## No-Telemetry Posture

`sdo-mcp` ships **no telemetry**. It does not phone home, collect usage
analytics, emit crash reports, or transmit anything to Scrape.do beyond the
scrape requests the model explicitly drives. There is no background reporting
channel and no UI — the only interfaces are stdin/stdout (JSON-RPC) and the
outbound Scrape.do request. This is a deliberate security property: the server
never becomes a side channel for your token or your targets. A change that
introduces any other outbound reporting is treated as a vulnerability.

## Supported Versions

We support the **latest released minor version**. Security fixes land on `main`
and ship in a new patch release of the current minor; we do not backport to older
minors. Upgrade to the latest minor to receive security updates.

| Version                | Supported          |
| ---------------------- | ------------------ |
| Latest minor (`x.y.*`) | :white_check_mark: |
| Any older minor        | :x:                |

If you are pinned to an older minor, the remediation for a reported vulnerability
is to upgrade to the patched latest minor (`go get -u
github.com/scrape-do/sdo-mcp`).

## Triage SLA

We are a small Solo+AI project; timelines are intentionally honest rather than
aspirational:

- **Acknowledgement:** within **3 business days** of receipt.
- **Initial assessment** (in scope? severity? reproducible?): within
  **7 business days** of acknowledgement.
- **Fix and coordinated release:** target **30 days** for High/Critical severity,
  **90 days** for Low/Moderate. If we expect to exceed these, we'll tell you and
  agree on a revised timeline.

Severity is assessed using CVSS v3.1, with weight given to token exposure and
to data/instruction boundary impact for this server's threat model.

We ask reporters to keep the issue private until a fixed release is available, or
until 90 days have passed if no fix has shipped — whichever comes first. We're
happy to coordinate a disclosure date with you.

## GHSA → CVE Process

We coordinate disclosure through GitHub's advisory system:

1. **Triage.** We confirm the report and assign a severity within the SLA above.
2. **Draft GHSA.** We open a private GitHub Security Advisory (GHSA) for the
   repository, listing affected versions, the fixed version, severity, and
   workarounds. The reporter is credited unless they ask to remain anonymous.
3. **Fix privately.** Where the change is sensitive, we develop and review the fix
   in a private fork tied to the advisory.
4. **Request CVE.** We use GitHub as a CVE Numbering Authority (CNA) to request a
   **CVE ID** through the advisory. The CVE is linked to the GHSA.
5. **Release.** We publish a patched release of the latest minor (a new Git tag),
   reference the advisory in the changelog, and retract affected versions with
   `retract` directives in `go.mod` where applicable.
6. **Publish.** We publish the GHSA — which propagates the CVE to the GitHub
   Advisory Database and the Go vulnerability database (`vuln.go.dev`) — so users
   running `govulncheck` and dependency scanners are alerted automatically.

## Credit

Unless you request otherwise, we credit reporters by name (or handle) in the
published advisory and release notes. We do not currently run a paid bug-bounty
program.

## License

This project is licensed under **Apache-2.0**. Reporting a vulnerability does not
transfer any rights; it simply helps us keep the server and its users safe.
