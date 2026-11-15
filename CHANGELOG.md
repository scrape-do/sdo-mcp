<!-- SPDX-License-Identifier: Apache-2.0 -->
# Changelog

Format: [Keep a Changelog](https://keepachangelog.com/) · [SemVer](https://semver.org/).

Before 1.0, a minor version may contain breaking changes; each is marked **Breaking** and described in MIGRATING.md. Patch versions never break.

## [0.3.1] - 2026-11-15

### Changed

- Built on `sdo-go` v0.2.1.

### Fixed

- The `scrape_plugin` tool description lists the documented
  `google/search/ai-overview`, `google/shopping/product/stores`, `google/lens`
  and `google/goto` routes.

## [0.3.0] - 2026-11-15

### Added

- Homebrew and Scoop: `brew install scrape-do/tap/sdo-mcp` (macOS and Linux)
  and `scoop bucket add scrape-do https://github.com/scrape-do/scoop-bucket`
  then `scoop install scrape-do/sdo-mcp` (Windows). Each final release updates
  the formula and the manifest, with the hashes from the release's signed
  `checksums.txt`; release candidates stay on GitHub only.

## [0.2.0] - 2026-11-04

### Added

- Async job tools on the `q.scrape.do` job API: `async_job_create`,
  `async_plugin_job_create` (one task per params entry, up to 1000),
  `async_job_status`, `async_task_result`, `async_jobs_list` (paged, filtered
  by state and start time, sorted), `async_job_cancel` and `async_account`.
  Job and task data from outside the server is fenced as untrusted; only the
  two create tools spend credits, and every tool description says whether a
  call does.

### Changed

- Built on `sdo-go` v0.2.0.

## [0.1.0] - 2026-11-01

### Added

- The first release of `sdo-mcp`, the standalone Scrape.do MCP server (the
  same server also ships inside the sdo CLI as `sdo mcp serve`): the `sdo-mcp`
  binary with a `serve` subcommand that speaks newline-framed JSON-RPC 2.0 over
  stdio, and `sdo-mcp version`, which prints the version the MCP handshake
  reports in `serverInfo`.
- The `scrape` tool: `url`, `render`, `super` (residential & mobile proxies),
  `geoCode`, `device`, `output`, `returnJson` (the page's network requests and
  content as JSON) and `sessionId` (a sticky proxy session, the same IP: an
  integer 0–1000000, as a number or a string of digits; an id out of range is
  an error before anything is sent). With a redirect the gateway did not
  follow, the result shows the target's `location`.
- The `scrape_plugin` tool, whose description lists the documented plugin
  routes and names `gl`/`hl` for Google plugins. It reports the response's
  status and cost (`status: 200`, `cost: 10`) like `scrape`, and describes a
  binary response rather than showing it. Its `params` values are scalars: a
  multi-value plugin parameter is one comma-separated string, since the
  plugins read only the first of a repeated key, and an array is a `-32602`
  error.
- Results: `Scrape.do-Request-Cost` decides origin (from sdo-go). A target's
  404 or 410, or any status of 400 or above the gateway billed (a positive
  cost: the target's own 400, 401, 403, 422, ...), is returned as the page,
  with its status and cost, and no `isError`. The gateway's own errors carry
  no cost (or 0) and are `isError` results carrying the gateway's message —
  its 400 (a rejected parameter), its 401 (a refused token, no credits) — or a
  plugin's `{"error", "message"}` envelope.
- Token: `serve --token`, `serve --token-file <path>` (the file's content,
  trimmed; refused on Unix when group or others can access it, so the token
  need not appear in `ps` the way a `--token` value does) or `SDO_TOKEN`, in
  that order. `SDO_TOKEN` or `--token-file` is the recommended way.
- `serve --client-timeout <seconds>` (default 70s: the gateway's own 60s
  default timeout plus 10s, so the gateway's answer always arrives; or
  `SDO_TIMEOUT`) and `serve --max-response-bytes <bytes>` bound a model-driven
  fetch's time and size.
- Limits: SIGINT/SIGTERM stops the server even while stdin stays open; a
  request line over 16 MiB is answered with a JSON-RPC invalid-request error
  (id `null`) and skipped; at most 4 tool calls run at once and 64 may be
  running or waiting, a call beyond that is refused with error `-32000`; a
  tool result is cut to its 128 KiB cap plus a margin before it is redacted
  and its marker look-alikes neutralized, so a 32 MiB page is cut in under
  50 MiB of memory, and a credential that straddles the cut is still masked
  whole.
- Builds with Go 1.26.9 or newer (`go.mod`, `sdo-go`'s floor), from a plain
  clone without a Go workspace.
- Release archives: a version tag builds `sdo-mcp` for linux, darwin and
  windows on amd64 and arm64 with GoReleaser, runs the binary from every
  archive on a runner of its platform, and publishes the archives with an SPDX
  SBOM each, `checksums.txt` and a cosign keyless signature for it to the
  GitHub Release after a maintainer's approval. See README "Install" and
  SECURITY.md.

[Unreleased]: https://github.com/scrape-do/sdo-mcp/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/scrape-do/sdo-mcp/compare/v0.3.0...v0.3.1
[0.3.0]: https://github.com/scrape-do/sdo-mcp/compare/v0.2.0...v0.3.0
[0.2.0]: https://github.com/scrape-do/sdo-mcp/compare/v0.1.0...v0.2.0
[0.1.0]: https://github.com/scrape-do/sdo-mcp/releases/tag/v0.1.0
