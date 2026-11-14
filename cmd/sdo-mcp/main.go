// SPDX-License-Identifier: Apache-2.0

// Command sdo-mcp is the standalone Scrape.do Model Context Protocol server.
// It speaks newline-framed JSON-RPC 2.0 over stdio; run it with `sdo-mcp serve`.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	sdo "github.com/scrape-do/sdo-go"
	"github.com/scrape-do/sdo-mcp/internal/cmdline"
	"github.com/scrape-do/sdo-mcp/internal/mcp"
	"github.com/scrape-do/sdo-mcp/internal/version"
)

// Exit codes. Scripts branch on these, so they are part of the contract.
const (
	exitOK      = 0
	exitFailure = 1 // the server loop failed
	exitUsage   = 2 // bad command line
	exitAuth    = 3 // no token
)

// maskToken redacts a message written before an sdo.Client exists (so Client.Redact
// is unavailable): it masks the literal token and its percent-encoded wire form
// when the token is long enough to mask safely, then the credential shapes via the
// SDK's package redactor — mirroring Client.Redact for the pre-client error path.
func maskToken(msg, tok string) string {
	if utf8.RuneCountInString(tok) >= 8 { // rune count, matching the SDK's redact()
		msg = strings.ReplaceAll(msg, tok, "***")
		if enc := url.QueryEscape(tok); enc != tok {
			msg = strings.ReplaceAll(msg, enc, "***")
		}
	}
	return sdo.RedactSecrets(msg)
}

// maxTokenFileBytes bounds what --token-file reads: a token is a short line,
// so a larger file is the wrong file.
const maxTokenFileBytes = 64 * 1024

// readTokenFile reads the token for --token-file, trimmed of surrounding
// whitespace. On Unix a file that group or others may access is refused, as
// sdo-cli refuses its config file: silently using a credential everyone can
// read would hide exactly the misconfiguration that matters. The mode is
// checked on the opened file, so it is the file actually read. Symlinks are
// followed (secret mounts are often links); the check applies to the target.
func readTokenFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("--token-file: %w", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("--token-file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("--token-file: %s is not a regular file", path)
	}
	// Windows does not carry POSIX permission bits, so only check where they mean something.
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("--token-file: %s is accessible by other users (mode %04o); run: chmod 600 %s",
			path, info.Mode().Perm(), path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxTokenFileBytes+1))
	if err != nil {
		return "", fmt.Errorf("--token-file: %w", err)
	}
	if len(data) > maxTokenFileBytes {
		return "", fmt.Errorf("--token-file: %s is larger than %d bytes; it should hold only the token", path, maxTokenFileBytes)
	}
	tok := strings.TrimSpace(string(data))
	if tok == "" {
		return "", fmt.Errorf("--token-file: %s is empty", path)
	}
	return tok, nil
}

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin *os.File, stdout, stderr *os.File) int {
	fs, flags := cmdline.NewServeFlags(stderr)

	usage := func() {
		fmt.Fprintln(stderr, "sdo-mcp — the standalone Scrape.do MCP server (stdio, JSON-RPC 2.0).")
		fmt.Fprintln(stderr, "\nUsage:\n  sdo-mcp serve [flags]\n  sdo-mcp version")
		fmt.Fprintln(stderr, "\nFlags:")
		fs.SetOutput(stderr)
		fs.PrintDefaults()
	}

	if len(args) == 0 {
		usage()
		return exitUsage
	}
	switch args[0] {
	case "serve":
		if err := fs.Parse(args[1:]); err != nil {
			// `serve --help` / `serve -h`: help was printed, that is success —
			// consistent with the top-level help below (exit 0, not a usage error).
			if errors.Is(err, flag.ErrHelp) {
				return exitOK
			}
			return exitUsage
		}
		if fs.NArg() > 0 {
			fmt.Fprintf(stderr, "sdo-mcp: unexpected argument %q\n", fs.Arg(0))
			return exitUsage
		}
	case "-h", "--help", "help":
		usage()
		return exitOK
	case "version":
		// The server's own version, the one the MCP handshake reports; the
		// release workflow runs this on every published binary.
		fmt.Fprintln(stdout, version.Version)
		return exitOK
	default:
		fmt.Fprintf(stderr, "sdo-mcp: unknown command %q\n", args[0])
		usage()
		return exitUsage
	}

	// Token: --token > --token-file > SDO_TOKEN. It is the sole credential and
	// is never logged.
	if flags.Token != "" && flags.TokenFile != "" {
		fmt.Fprintln(stderr, "sdo-mcp: pass --token or --token-file, not both")
		return exitUsage
	}
	tok := flags.Token
	if tok == "" && flags.TokenFile != "" {
		var err error
		if tok, err = readTokenFile(flags.TokenFile); err != nil {
			fmt.Fprintln(stderr, "sdo-mcp: "+sdo.RedactSecrets(err.Error()))
			return exitAuth
		}
	}
	if tok == "" {
		tok = os.Getenv("SDO_TOKEN")
	}
	if tok == "" {
		fmt.Fprintln(stderr, sdo.RedactSecrets("sdo-mcp: no API token: set SDO_TOKEN or pass --token-file (or --token)"))
		return exitAuth
	}

	// The gateway sees the sdo-go SDK's own User-Agent (sdo-sdk-go/<ver>) — the
	// single client-identifying header. sdo-mcp is a Go SDK consumer and keeps
	// that identity rather than minting a new one.
	var opts []sdo.Option
	if flags.InsecureHTTP {
		opts = append(opts, sdo.WithAllowInsecureHTTP())
	}
	// --client-timeout and --max-response-bytes are applied only when explicitly
	// given, so an absent flag still lets SDO_TIMEOUT (timeout) and the SDK's
	// 32 MiB default (size) apply. Precedence for the timeout is
	// flag > SDO_TIMEOUT > the 70s default, because WithTimeout is passed only here.
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	if set["client-timeout"] {
		if flags.ClientTimeout < 0 {
			fmt.Fprintln(stderr, "sdo-mcp: --client-timeout must be >= 0 (0 disables the timeout)")
			return exitUsage
		}
		opts = append(opts, sdo.WithTimeout(time.Duration(flags.ClientTimeout*float64(time.Second))))
	}
	if set["max-response-bytes"] {
		if flags.MaxResponseBytes <= 0 {
			fmt.Fprintln(stderr, "sdo-mcp: --max-response-bytes must be a positive number of bytes")
			return exitUsage
		}
		opts = append(opts, sdo.WithMaxResponseBytes(flags.MaxResponseBytes))
	}
	c, err := sdo.New(tok, opts...)
	if err != nil {
		// No client yet to mask the literal token, so do it here (defense in
		// depth). The SDK's own ConfigErrors never echo the token, but a future
		// error might.
		fmt.Fprintln(stderr, "sdo-mcp: "+maskToken(err.Error(), tok))
		return exitFailure
	}

	// Cancel the server context on SIGINT/SIGTERM so an interrupt abandons any
	// in-flight gateway request and shuts the loop down cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := mcp.Serve(ctx, c, stdin, stdout); err != nil {
		fmt.Fprintln(stderr, "sdo-mcp: "+c.Redact(err.Error()))
		return exitFailure
	}
	return exitOK
}
