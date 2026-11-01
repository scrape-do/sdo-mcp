# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-11-01

### Added

- Initial standalone release of `sdo-mcp`, the Scrape.do MCP server, split out
  of the sdo CLI (previously `sdo mcp serve`). Ships as the `sdo-mcp` binary
  with a `serve` subcommand that speaks newline-framed JSON-RPC 2.0 over stdio.
- `serve --client-timeout <seconds>` and `serve --max-response-bytes <bytes>`
  flags, so operators can bound a model-driven fetch's time and size. These were
  referenced by the server's error messages but not actually wired on the
  standalone binary after the split from the CLI.
- `serve --token-file <path>` reads the token from a file (trimmed; refused on
  Unix when group or others can access it), so it need not appear in `ps` the
  way a `--token` value does. `SDO_TOKEN` or `--token-file` is now the
  recommended way to pass the token.
- `sdo-mcp version` prints the server's version (the one the MCP handshake
  reports in `serverInfo`).
- Release archives: a version tag builds `sdo-mcp` for linux, darwin and
  windows on amd64 and arm64 with GoReleaser, runs the binary from every archive
  on a runner of its platform, and publishes the archives with an SPDX SBOM
  each, `checksums.txt` and a cosign keyless signature for it to the GitHub
  Release after a maintainer's approval. See README "Install" and SECURITY.md.

### Changed

- `--client-timeout` defaults to 70s (was 60s): the gateway's own default
  timeout is 60s, so the client now waits 10s longer and the gateway's answer
  always arrives. `SDO_TIMEOUT` and an explicit flag keep their meaning.
- **Behavior change.** `Scrape.do-Request-Cost` decides origin (from sdo-go):
  any status of 400 or above the gateway billed (a positive cost — the target's
  own 400, 401, 403, 422, ...) is now returned like a target 404 or 410: the
  page, with its status and cost, and no `isError`. The gateway's own errors
  carry no cost (or 0) and stay `isError` results carrying the gateway's
  message — its 400 (a rejected parameter), its 401 (a refused token, no
  credits) — and a plugin's `{"error", "message"}` envelope now supplies its
  message too.

- **Behavior change.** `scrape_plugin` reports the response's status and cost
  (`status: 200`, `cost: 10`) like `scrape`, and a binary plugin response is
  described rather than shown. Its `params` values are scalars only: a
  multi-value plugin parameter is one comma-separated string (the plugins read
  only the first of a repeated key, so an array silently lost values); an
  array is now a `-32602` error. The tool description lists the documented
  plugin routes and names `gl`/`hl` for Google plugins.
- `scrape`'s `sessionId` accepts an integer as well as a string, and an id
  outside 0–1000000 is an error before anything is sent; its result shows the
  target's `location` when a redirect was not followed.

### Fixed

- The `scrape` tool's descriptions: `returnJson` returns the page's network
  requests and content as JSON (it does not "parse the response"), and
  `sessionId` is a sticky proxy session (the same IP), an integer 0–1000000,
  not "any id you choose"; `super` is residential & mobile proxies.
- SIGINT/SIGTERM now stops the server even while stdin stays open; before, the
  loop only noticed the signal after the client closed its end.
- A request line over the 16 MiB limit no longer ends the server: it is answered
  with a JSON-RPC invalid-request error (id `null`) and skipped.
- At most 64 tool calls may be running or waiting at once (4 still run
  concurrently); a call beyond that is refused with error `-32000` instead of
  queueing another goroutine without bound.
- A tool result is cut to its 128 KiB cap plus a margin before it is redacted
  and its marker look-alikes neutralized, so a 32 MiB page no longer costs
  gigabytes of memory on the way to being cut (over 6 GiB allocated before,
  under 50 MiB now); a credential that straddles the cut is still masked whole.
- The checkout builds again: `go.mod` declares `go 1.26.9`, `sdo-go`'s floor,
  and the committed `go.work` (whose `go 1.26.0` line was below that floor) is
  gone. `go.work` and `go.work.sum` are now
  gitignored; CONTRIBUTING.md and the README describe the local workspace.
