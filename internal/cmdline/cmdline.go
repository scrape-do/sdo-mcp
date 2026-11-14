// SPDX-License-Identifier: Apache-2.0

// Package cmdline declares sdo-mcp's command line: its commands and the flags
// of `sdo-mcp serve`. cmd/sdo-mcp parses with it, and tools/apisurface lists
// it in api-surface.txt, so the two cannot disagree.
package cmdline

import (
	"flag"
	"io"
)

// Name is the binary's name, which `go install` takes from cmd/sdo-mcp.
const Name = "sdo-mcp"

// Commands are the subcommands sdo-mcp runs; "help" also answers to -h and
// --help.
var Commands = []string{"help", "serve", "version"}

// Serve holds the parsed flags of `sdo-mcp serve`.
type Serve struct {
	Token            string
	TokenFile        string
	InsecureHTTP     bool
	ClientTimeout    float64
	MaxResponseBytes int64
}

// NewServeFlags defines the flags of `sdo-mcp serve` on a new FlagSet that
// writes its errors and help to out, and returns where they are parsed to.
func NewServeFlags(out io.Writer) (*flag.FlagSet, *Serve) {
	fs := flag.NewFlagSet(Name+" serve", flag.ContinueOnError)
	fs.SetOutput(out)
	s := &Serve{}
	fs.StringVar(&s.Token, "token", "", "Scrape.do API token; visible to other local users in the process list, so prefer SDO_TOKEN or --token-file")
	fs.StringVar(&s.TokenFile, "token-file", "", "read the Scrape.do API token from this file (owner-only permissions on Unix)")
	fs.BoolVar(&s.InsecureHTTP, "insecure-http", false,
		"allow a plain-http SDO_BASE_URL / SDO_ASYNC_BASE_URL on a non-loopback host (sends the token in cleartext)")
	fs.Float64Var(&s.ClientTimeout, "client-timeout", 0,
		"per-attempt timeout in seconds; 0 disables it (default: 70, or SDO_TIMEOUT)")
	fs.Int64Var(&s.MaxResponseBytes, "max-response-bytes", 0,
		"ceiling for a decompressed response body, in bytes (default: 33554432, i.e. 32 MiB)")
	return fs, s
}
