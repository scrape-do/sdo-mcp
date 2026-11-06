# sdo-mcp

The standalone **Scrape.do MCP server**: a minimal [Model Context
Protocol](https://modelcontextprotocol.io) server that exposes the Scrape.do
scraping surface to an LLM. It speaks newline-framed JSON-RPC 2.0 over stdio and
reuses the [sdo Go SDK](https://github.com/scrape-do/sdo-go) underneath.

It was split out of the [sdo CLI](https://github.com/scrape-do/sdo-cli), where
it previously shipped as `sdo mcp serve`.

## Tools

- `scrape` — fetch a URL through Scrape.do and return the page content, fenced
  as untrusted third-party data.
- `scrape_plugin` — call a documented Scrape.do plugin endpoint (e.g.
  `google/search`, `amazon/pdp`, `walmart/product`, `chatgpt/chat`; the tool
  description lists the documented routes) and return its status, cost and
  output, fenced as untrusted third-party data. Plugin params are scalars: a
  parameter that takes several values (`hotel_class`, `departure_id`, ...) is
  one comma-separated string (`"4,5"`), since the plugins read only the first
  of a repeated key; an array is refused. Google plugins take `gl` for the
  country, not `geoCode`.
- Async jobs on `q.scrape.do`, for batches that should not block the model:
  - `async_job_create` (`targets`, `options`) and `async_plugin_job_create`
    (`plugin`, `params`: one object per task, up to 1000; `options`) submit a
    job and return its id and task ids at once. These spend credits.
  - `async_job_status` (`jobId`) and `async_jobs_list` (`page`, `pageSize`,
    `status`, `startFrom`, `startTo`, `sort`; the reply says how many pages
    there are) report jobs.
  - `async_task_result` (`jobId`, `taskId`) returns a finished task's status
    code and content; a task still running reports its state only.
  - `async_job_cancel` (`jobId`) and `async_account` (async concurrency,
    active jobs, credits) complete the set.
  `options` takes the async API's own PascalCase job keys (`Super`, `GeoCode`,
  `Render`, `WebhookURL`, ...). Everything a job or task carries from outside
  (target URLs, error messages, content) is fenced as untrusted data. Only the
  two create tools spend credits; every description says which.

The `scrape` tool takes `url`, `render`, `super`, `geoCode`, `device`,
`output`, `returnJson` (the page's network requests and content as JSON) and
`sessionId` (a sticky proxy session, the same IP across calls: an integer
0–1000000, as a number or a string of digits). With a redirect the gateway did
not follow, its result shows the target's `location`.

There is no usage-statistics tool on purpose: the scrape tools fetch content for
the model, and account figures are an operator's concern (`sdo usage` in the
CLI, or `Client.Usage` in the SDK). The `/info` endpoint also allows only 10
calls a minute per IP, a budget a model in a loop would spend for the user's
other tools.

A page the target answered — including a 404 or 410, or any status of 400 or
above the gateway billed (a positive `Scrape.do-Request-Cost`, the
authoritative charge) — is a normal result showing its status and cost. Only
Scrape.do's own errors (no cost, or 0: a rejected parameter, a refused token,
a failed fetch) are returned with `isError: true`.

Scraped content and gateway/target error text are treated as untrusted data,
not instructions: every result is redacted (the API token is never surfaced) and
fenced with nonce-delimited markers before it reaches the model.

## Install

With [Homebrew](https://brew.sh) (macOS and Linux), from the
[scrape-do/homebrew-tap](https://github.com/scrape-do/homebrew-tap) tap:

```sh
brew install scrape-do/tap/sdo-mcp
brew upgrade sdo-mcp             # later, for a new release
```

With [Scoop](https://scoop.sh) (Windows), from the
[scrape-do/scoop-bucket](https://github.com/scrape-do/scoop-bucket) bucket:

```powershell
scoop bucket add scrape-do https://github.com/scrape-do/scoop-bucket
scoop install sdo-mcp
scoop update sdo-mcp             # later, for a new release
```

Both carry final releases only (no release candidates) and install the same
archives as the GitHub Releases page, checked against the release's signed
`checksums.txt` (see [SECURITY.md](SECURITY.md#supply-chain-integrity)).

Or download a prebuilt archive from the
[GitHub Releases](https://github.com/scrape-do/sdo-mcp/releases) page. For
version X.Y.Z there is `sdo-mcp_X.Y.Z_<os>_<arch>.tar.gz` for `linux` and
`darwin` and `sdo-mcp_X.Y.Z_windows_<arch>.zip` for Windows, each for `amd64`
and `arm64`. Every archive holds the `sdo-mcp` binary (`sdo-mcp.exe` on
Windows), `LICENSE`, `NOTICE`, `README.md`, `CHANGELOG.md` and
`THIRD_PARTY_LICENSES.txt`; every release also carries `checksums.txt` and a
cosign signature for it (see [SECURITY.md](SECURITY.md) for how to verify a
download). Unpack it and put `sdo-mcp` on your `PATH`.

With a Go toolchain (1.26 or newer) you can instead install from source:

```sh
go install github.com/scrape-do/sdo-mcp/cmd/sdo-mcp@latest   # installs a binary named sdo-mcp
```

or build a clone with `go build -o sdo-mcp ./cmd/sdo-mcp`. `sdo-mcp version`
prints the installed version.

## Running

```sh
sdo-mcp serve
```

The server reads JSON-RPC requests on stdin and writes replies on stdout, one
per line. Point your MCP client at that command. It runs until stdin is closed
or it receives SIGINT/SIGTERM, which cancels any running tool call. A request
line longer than 16 MiB is answered with a JSON-RPC error (id `null`) and
skipped. At most 4 tool calls run at once; once 64 are running or waiting, a
further call is refused with error code `-32000` until one finishes.

### Token

The API token is the sole credential and is never logged. It is resolved from,
in order:

1. `--token <token>`
2. `--token-file <path>` — the file's content, trimmed of surrounding
   whitespace. On Unix the file must not be accessible by group or others
   (`chmod 600`); the server refuses it otherwise.
3. the `SDO_TOKEN` environment variable

Prefer `SDO_TOKEN` or `--token-file`: a `--token` value is visible to every
local user in the process list (`ps`). `--token` and `--token-file` cannot be
combined. If no token is found, or the token file cannot be used, the server
prints an error and exits with code 3.

### Flags

- `--token` — Scrape.do API token (visible in `ps`; prefer `SDO_TOKEN`).
- `--token-file <path>` — read the token from an owner-only file.
- `--insecure-http` — allow a plain-http `SDO_BASE_URL` / `SDO_ASYNC_BASE_URL`
  on a non-loopback host (sends the token in cleartext).
- `--client-timeout <seconds>` — per-attempt timeout; `0` disables it. Defaults
  to 70s (the gateway's own 60s default timeout plus 10s), or the
  `SDO_TIMEOUT` environment variable when the flag is absent.
  `--client-timeout 0` leaves each attempt unbounded (intended for debugging):
  combined with retries, a slow or hung gateway can stall the call indefinitely,
  so prefer a finite value outside debugging.
- `--max-response-bytes <bytes>` — ceiling for a decompressed response body; a
  larger response fails the call rather than being materialized. Defaults to
  33554432 (32 MiB). Useful to bound how much a model-driven fetch can pull.

## License

Apache-2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
