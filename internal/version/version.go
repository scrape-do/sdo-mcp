// SPDX-License-Identifier: Apache-2.0

// Package version holds the sdo-mcp server's own version (reported in the MCP
// handshake's serverInfo). Outbound gateway requests are identified by the sdo-go
// SDK's own User-Agent (sdo-sdk-go/<ver>) — the only client-identifying header —
// so sdo-mcp does not mint a User-Agent of its own.
package version

const Version = "0.1.0"
