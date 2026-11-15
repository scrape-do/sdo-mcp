<!-- SPDX-License-Identifier: Apache-2.0 -->
# Migrating

This document records breaking changes between releases of `sdo-mcp`, the
standalone Scrape.do MCP server. Each entry explains what changed, why, and the
exact steps to update your MCP client configuration or the agents that use the
tools.

`sdo-mcp` follows [Semantic Versioning](https://semver.org/). Before 1.0, a
minor version may contain breaking changes; each is marked **Breaking** in
[CHANGELOG.md](CHANGELOG.md) and described here. Patch versions never break.
After `1.0.0`, breaking changes are confined to major versions.

## What counts as a breaking change

The server is driven by MCP clients and by the models behind them, so both the
command line and the tool surface are part of the contract:

- **Tools.** Removing or renaming a tool, removing or renaming one of its
  arguments, making an optional argument required, narrowing an argument's
  type or enum, or changing the shape of a tool result (the order and meaning
  of its content items, the metadata lines, the JSON fields of a job or task)
  is a breaking change. Adding a tool, adding an optional argument, or adding a
  field to a JSON result is **not**.
- **Command line.** Removing or renaming `sdo-mcp serve` or one of its flags,
  changing a flag's type or default, or changing an exit code (`0` success,
  `1` server failure, `2` usage error, `3` authentication error) is a breaking
  change.
- **Token resolution.** The token is read from `--token`, then `--token-file`,
  then `SDO_TOKEN`, and from nowhere else. Changing that order or adding another
  credential source is a breaking change.
- **Protocol.** Dropping support for an MCP protocol version the server
  negotiates today is a breaking change; adding a newer one is not.
- **Install path.** `go install github.com/scrape-do/sdo-mcp/cmd/sdo-mcp`
  builds the binary; moving that package renames the binary and is a breaking
  change.

The command, flag and tool surface is pinned by `api-surface.txt`, so a change
to it cannot land unnoticed.

Stability guarantees that will not change without a migration section here:

- **No telemetry.** The server contacts nothing but the Scrape.do API.
- **No UI.** `sdo-mcp` speaks JSON-RPC over stdio; there is no web or desktop
  interface.
- **Token-only auth.** The API token is the single credential, and it is the
  server's, never the model's: no tool argument carries it.

## Parity with the sdo CLI

The same server ships inside the [sdo CLI](https://github.com/scrape-do/sdo-cli)
as `sdo mcp serve`; the two copies are kept identical and released together, so
a breaking change here has a matching entry in sdo-cli's MIGRATING.md. The
request and response behaviour comes from the `sdo-go` SDK, validated by the
conformance corpus shared with the other sdo SDKs.

## Governance

`sdo-mcp` uses Solo+AI governance: the founder is the sole approver.
Contributions are accepted under the
[Developer Certificate of Origin](https://developercertificate.org/) (DCO) with
a `Signed-off-by` trailer; there is no CLA. Licensed under Apache-2.0.

---

## Releases

No release so far has required action from you: 0.2.0 and 0.3.0 only added
tools and distribution channels. Each later release that does gets a section
here, newest first; read every section between your current version and your
target version.
