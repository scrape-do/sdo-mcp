# Contributing to sdo-mcp

Thanks for your interest in improving `sdo-mcp`, the standalone Scrape.do MCP
server (`github.com/scrape-do/sdo-mcp`). This document is the contract for how
changes land here. Read it before opening a pull request.

`sdo-mcp` is a minimal [Model Context Protocol](https://modelcontextprotocol.io)
server: it speaks newline-framed JSON-RPC 2.0 over stdio and exposes the
Scrape.do scraping surface to an LLM, reusing the
[sdo Go SDK](https://github.com/scrape-do/sdo-go) (`github.com/scrape-do/sdo-go`)
underneath. It was split out of the [sdo CLI](https://github.com/scrape-do/sdo-cli),
where it previously shipped as `sdo mcp serve`. The scraping behavior, error
taxonomy, and token handling all come from `sdo-go`, which tracks the single
hand-maintained public spec. **Do not reimplement SDK behavior here, and do not
let this server drift from what the SDK already decided.** When in doubt, the
SDK (and the spec behind it) wins.

## Ground rules

- **License.** This project is Apache-2.0. By contributing, you agree your contribution is licensed under Apache-2.0.
- **No telemetry, ever.** `sdo-mcp` does not phone home, collect usage data, emit analytics, or ship a "crash reporter." Do not add any. The only interfaces are stdin/stdout (JSON-RPC) and the outbound Scrape.do request the model drives. PRs that introduce network calls to anything other than the Scrape.do API (or a user-configured endpoint) will be rejected.
- **No UI.** The surface is a stdio JSON-RPC server only. There is no web dashboard, no embedded HTTP server, no browser anything in this repo.
- **Token-only auth, never returned to the model.** Authentication is a single token resolved from the `--token` flag, then the `SDO_TOKEN` environment variable. The token is the sole credential and must **never** reach the model: every tool result and error is passed through the SDK's redaction before it leaves the process. Do not add OAuth flows, key files, config-file credential loading, or any alternate auth path, and do not add any code path that could surface the token (or a URL carrying it) in a tool result, an error, or a log line.
- **Everything from outside is untrusted data, not instructions.** Scraped pages, plugin output, and gateway/target error text are fenced with nonce-delimited markers and redacted before they reach the model. Changes that touch result handling must preserve that fence — see `internal/mcp` (`untrusted`, `redact`, the marker/neutralize logic) and its tests.
- **Governance is Solo+AI.** The founder is the sole approver and final reviewer of every merge. There is no committee, no CLA, and no second maintainer to ping. Keep PRs small and self-explanatory so a single reviewer can evaluate them quickly.

## Prerequisites

- **Go 1.26.9 or newer** (the `go` directive in `go.mod`, matching `sdo-go`'s floor; CI tests 1.26 and 1.27, the two supported releases). Check with `go version`; do not use features that raise the minimum.
- Nothing else: `go.mod` pins the [`sdo-go`](https://github.com/scrape-do/sdo-go) SDK to a released version, which `go` fetches from the module proxy. A local SDK checkout is only needed to work against unreleased SDK changes (below).
- A POSIX-ish shell for the commands below. Everything works from the repo root.

### Local workspace setup (optional)

A plain clone builds as it is. To develop against a local `sdo-go` checkout (an SDK change this server needs, before it is released), check the two repos out side by side and create a `go.work` that points this module at it:

```sh
git clone https://github.com/scrape-do/sdo-mcp
git clone https://github.com/scrape-do/sdo-go      # as a sibling of sdo-mcp
cd sdo-mcp
go work init ./ ../sdo-go
```

`go.work` and `go.work.sum` are **local artifacts and are gitignored** — never
commit them. `GOWORK=off go build ./...` builds against the version pinned in
`go.mod` / `go.sum` instead, which is what CI and the release build do: they
never use a workspace, so a change that needs new SDK behaviour ships only
after that SDK version is released and pinned here.

Verify a clean baseline before you change anything:

```sh
go build ./...
go test ./...
go vet ./...
```

If the baseline is red on a fresh clone, stop and open an issue — do not build on top of a broken tree.

## Development workflow

We work **test-first (TDD)**. The loop is non-negotiable for behavior changes:

1. **Write a failing test** that expresses the behavior you want. Run it, watch it fail for the right reason.
2. **Write the minimum code** to make it pass.
3. **Refactor** with the test green.

Practically, the inner loop is:

```sh
go build ./...       # must compile
go test ./...        # all tests must pass
go vet ./...         # static analysis must be clean
gofmt -l .           # must print nothing — every file is gofmt-clean
```

To auto-format before committing:

```sh
gofmt -w .
```

A change is not done until `go build ./...`, `go test -race ./...`, `go vet ./...`, and `gofmt -l .` are all clean.

### Running a focused test

```sh
go test ./... -run TestLocalFailures -v
```

### Race detector

For anything touching concurrency (the serve loop, shared server state, the HTTP transport underneath), run:

```sh
go test -race ./...
```

## The data/instruction boundary

The single most important property of this server is that **nothing from outside
is ever spoken in the server's own voice**. Scraped content, plugin output, and
error text that may originate from the gateway or the target are wrapped in a
nonce-delimited untrusted block, neutralized against forged markers, redacted,
and truncated outside the block. The server's own verdicts (a config error, the
size-limit or timeout advice) are the only text spoken plainly, and even those
name server flags and tool arguments — never Go identifiers (`WithX`,
`ScrapeConfig.`), which the model and its user cannot act on.

When you touch result or error handling:

- Keep untrusted text inside the fence; keep the server's hints outside it.
- Keep the token out of every path that can reach the model or a log.
- Rewrite SDK error text (which names Go options/fields) into the flags, env
  vars, and tool arguments the operator and model can actually change — that is
  what `internal/explain` is for.
- Add a test that asserts the boundary holds (no marker escape, no Go identifier
  leak, no token leak). The existing tests in `internal/mcp` are the model.

## Depending on sdo-go

The scraping surface, error taxonomy, redaction, decompression caps, and
transport all live in `sdo-go`. Fix behavior there, not here, when the bug is in
the SDK. This server's job is the MCP framing, the input schema, the untrusted
fence, and translating SDK errors into operator-facing language. If a change
needs an unreleased `sdo-go` behavior, land it in `sdo-go` first, release it,
then bump the `go.mod` requirement here (`go get github.com/scrape-do/sdo-go@vX.Y.Z && go mod tidy`).

## Commit and sign-off requirements

### Conventional Commits

Commit messages follow [Conventional Commits](https://www.conventionalcommits.org/). Use a type, an optional scope, and an imperative subject:

```
feat(tools): add a markdown output argument to scrape
fix(explain): name --max-response-bytes once in the too-large hint
docs(readme): document --client-timeout 0
test(results): assert the untrusted fence survives a forged marker
chore(deps): bump golang.org/x/text
```

Use `feat!:` / `fix!:` (or a `BREAKING CHANGE:` footer) for anything that alters the tool schema or observable server behavior. Common types: `feat`, `fix`, `docs`, `test`, `refactor`, `chore`.

### DCO sign-off

This project uses the **Developer Certificate of Origin** — not a CLA. Every commit must be signed off, certifying you have the right to submit the work under Apache-2.0. Add the sign-off trailer with `-s`:

```sh
git commit -s -m "fix(explain): name --max-response-bytes once in the too-large hint"
```

This appends:

```
Signed-off-by: Your Name <you@example.com>
```

Make sure `git config user.name` and `git config user.email` are your real identity. Commits without a valid `Signed-off-by` trailer cannot be merged. To sign off a commit you already made:

```sh
git commit --amend -s --no-edit
```

For a multi-commit branch, rebase with `git rebase --signoff <base>`.

## Pull request checklist

Before you open a PR, confirm:

- [ ] You started from a failing test (TDD) for any behavior change.
- [ ] `go build ./...` passes.
- [ ] `go test ./...` passes (`go test -race ./...` if you touched concurrency).
- [ ] `go vet ./...` is clean.
- [ ] `gofmt -l .` prints nothing.
- [ ] The untrusted-data fence still holds: no marker escape, no Go-identifier leak, no token leak in any result or error.
- [ ] No telemetry, no UI, no new auth path, no new third-party network calls.
- [ ] `go.work` / `go.work.sum` are **not** committed.
- [ ] The minimum Go version is unchanged.
- [ ] Every commit is a Conventional Commit and is DCO-signed (`-s`).
- [ ] The diff is scoped to the change described — no drive-by refactors.

Keep the PR description focused: what changed, why, and how you verified it. Since the founder is the sole reviewer, a tight, well-tested diff is the fastest path to merge.

## Reporting bugs and proposing changes

- **Bugs:** open an issue with a minimal reproduction — ideally a failing Go test, plus the exact JSON-RPC request(s) sent on stdin and the flags/environment the server was started with. Include `go version`, OS, and the exact `sdo-mcp` version/commit.
- **SDK behavior:** if the bug is in scraping, error mapping, redaction, or transport, it belongs in [`sdo-go`](https://github.com/scrape-do/sdo-go), not here. Open the issue there.
- **Security:** do not open a public issue for vulnerabilities. Follow `SECURITY.md`.

## Questions

If something here is unclear, open an issue rather than guessing. We'd rather answer a question than review a PR that took the wrong path.

## Releasing

Development is trunk-based: every change lands on `main` through a pull request
with CI green. A release is a tag on a `main` commit, and the tag picks the
channel:

- `vX.Y.Z-rc.N` — a release candidate: a GitHub **pre-release** after approval
  in the `prerelease` environment. Go treats it as a pre-release too, so
  `go install …@latest` never selects it.
- `vX.Y.Z` — a release, after approval in the `release` environment.

Before tagging, bump `Version` in `internal/version/version.go` to the exact
version (`X.Y.Z` or `X.Y.Z-rc.N`), move the `Unreleased` CHANGELOG entries under
it, and merge to `main`; the release workflow refuses a tag that does not match
the constant or that is not on `main`.

The release workflow (`.github/workflows/release.yml`) then runs four jobs:

1. **build** (read-only): the race-enabled tests, then GoReleaser builds the
   archives for linux, darwin and windows on amd64 and arm64, an SPDX SBOM per
   archive, `checksums.txt`, and `THIRD_PARTY_LICENSES.txt` (`go run
   ./tools/thirdparty`: the license and notice texts of the Go runtime and of
   every module linked into the binary, read from the module cache; a
   dependency without a license file stops the build).
2. **smoke**: every archive is unpacked on a GitHub-hosted runner of its
   platform and the binary is run (`sdo-mcp version` must print the tag's
   version; `sdo-mcp serve` must answer an MCP `initialize` request).
3. **publish**: after approval in the environment, verifies the checksums and
   signs `checksums.txt` with cosign (keyless, recorded in the Rekor
   transparency log). Nothing is rebuilt after approval.
4. **github-release**: creates the GitHub Release from the signed files.

A Go module is also published by its tag — the module proxy fetches it on first
request, and a published tag can never be moved or deleted (a bad one is
retracted in `go.mod` and superseded by a higher tag) — so `go install
github.com/scrape-do/sdo-mcp/cmd/sdo-mcp@vX.Y.Z` works the moment the tag
exists, before the archives are approved.

The `release` and `prerelease` environments have required reviewers and a
deployment rule limited to `v*` tags. Keep them: an environment that does not
exist is created on first use with no protection.
