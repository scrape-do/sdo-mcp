// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/scrape-do/sdo-mcp/internal/testenv"
	"github.com/scrape-do/sdo-mcp/internal/version"
)

// TestMain strips SDO_* once for the whole package so an ambient SDO_BASE_URL /
// SDO_ASYNC_BASE_URL / SDO_TIMEOUT in the developer's shell cannot leak into
// sdo.New and turn the serve tests red (the sibling internal/mcp does the same).
func TestMain(m *testing.M) {
	testenv.Clear()
	os.Exit(m.Run())
}

// TestMaskToken exercises the pre-client redaction directly (the integration test
// below cannot: sdo.New's ConfigError never embeds the token, so it would pass
// even with the masking removed). This one fails if the masking regresses.
func TestMaskToken(t *testing.T) {
	tok := "abc123def456ghi" // >= 8
	if got := maskToken("connect to https://x/?y=1 boom "+tok+" boom", tok); strings.Contains(got, tok) {
		t.Errorf("literal token leaked: %q", got)
	}
	special := "tok/with+special=chars99"
	enc := url.QueryEscape(special)
	if got := maskToken("url ?token="+enc, special); strings.Contains(got, enc) {
		t.Errorf("percent-encoded token leaked: %q", got)
	}
	// A <8 token must not shred prose, but the credential SHAPE is still masked.
	if got := maskToken("prose short here", "short"); got != "prose short here" {
		t.Errorf("short token shredded prose: %q", got)
	}
	if got := maskToken("token=short&x=1", "short"); strings.Contains(got, "short") {
		t.Errorf("token= shape not masked for a short token: %q", got)
	}
}

// runCapture drives run() with the given args, an already-EOF stdin (so a serve
// loop ends at once without networking), and returns the exit code plus captured
// stdout/stderr. Each test sets SDO_TOKEN explicitly via t.Setenv, so the ambient
// environment cannot change the outcome.
func runCapture(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	inW.Close() // immediate EOF
	outF, err := os.CreateTemp(t.TempDir(), "out")
	if err != nil {
		t.Fatal(err)
	}
	defer outF.Close()
	errF, err := os.CreateTemp(t.TempDir(), "err")
	if err != nil {
		t.Fatal(err)
	}
	defer errF.Close()
	code := run(args, inR, outF, errF)
	inR.Close()
	outB, _ := os.ReadFile(outF.Name())
	errB, _ := os.ReadFile(errF.Name())
	return code, string(outB), string(errB)
}

func TestRun_HelpExitsZero(t *testing.T) {
	code, _, errOut := runCapture(t, "help")
	if code != exitOK {
		t.Fatalf("help exit=%d, want %d", code, exitOK)
	}
	if !strings.Contains(errOut, "Usage") {
		t.Errorf("help printed no usage: %q", errOut)
	}
}

func TestRun_VersionPrintsTheServerVersion(t *testing.T) {
	code, out, errOut := runCapture(t, "version")
	if code != exitOK {
		t.Fatalf("version exit=%d, want %d (stderr %q)", code, exitOK, errOut)
	}
	if out != version.Version+"\n" {
		t.Errorf("version printed %q, want %q", out, version.Version+"\n")
	}
}

func TestRun_ServeHelpExitsZero(t *testing.T) {
	// `serve --help` prints flags and succeeds (flag.ErrHelp → exit 0), matching
	// top-level help — not a usage error.
	if code, _, _ := runCapture(t, "serve", "--help"); code != exitOK {
		t.Errorf("serve --help exit=%d, want %d", code, exitOK)
	}
}

func TestRun_NoArgsAndUnknownCommandAreUsageErrors(t *testing.T) {
	if code, _, _ := runCapture(t); code != exitUsage {
		t.Errorf("no args exit=%d, want %d", code, exitUsage)
	}
	if code, _, errOut := runCapture(t, "bogus"); code != exitUsage {
		t.Errorf("unknown command exit=%d, want %d", code, exitUsage)
	} else if !strings.Contains(errOut, "unknown command") {
		t.Errorf("no unknown-command message: %q", errOut)
	}
	if code, _, _ := runCapture(t, "serve", "extra-arg"); code != exitUsage {
		t.Errorf("unexpected serve arg exit=%d, want %d", code, exitUsage)
	}
}

func TestRun_NoTokenExitsAuth(t *testing.T) {
	t.Setenv("SDO_TOKEN", "")
	code, _, errOut := runCapture(t, "serve")
	if code != exitAuth {
		t.Fatalf("no-token exit=%d, want %d", code, exitAuth)
	}
	if !strings.Contains(errOut, "token") {
		t.Errorf("no-token error gives no hint: %q", errOut)
	}
}

func TestRun_InvalidTokenIsNotPrinted(t *testing.T) {
	// A token with an interior control byte fails sdo.New's validation; the error
	// path must not echo the token value (exercises the literal-redaction block).
	t.Setenv("SDO_TOKEN", "")
	bad := "abc\x01defghij" // >= 8 chars, invalid (control char)
	code, _, errOut := runCapture(t, "serve", "--token", bad)
	if code != exitFailure {
		t.Fatalf("invalid-token exit=%d, want %d", code, exitFailure)
	}
	if strings.Contains(errOut, bad) {
		t.Errorf("token leaked into error output: %q", errOut)
	}
}

func TestRun_TokenFromEnvServesUntilEOF(t *testing.T) {
	// A valid token via SDO_TOKEN builds the client; EOF stdin ends the loop.
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	code, _, _ := runCapture(t, "serve")
	if code != exitOK {
		t.Fatalf("serve exit=%d, want %d", code, exitOK)
	}
}

func TestRun_TokenFlagBeatsEnv(t *testing.T) {
	// --token takes precedence over the env; the serve loop returns on EOF.
	t.Setenv("SDO_TOKEN", "")
	code, _, _ := runCapture(t, "serve", "--token", "flag-token-9876")
	if code != exitOK {
		t.Fatalf("serve with --token exit=%d, want %d", code, exitOK)
	}
}

func TestRun_TimeoutAndSizeFlagsAreAccepted(t *testing.T) {
	// The flags the error messages name are real: valid values build the client
	// and the loop returns on EOF. 0 is a legal --client-timeout (disables it).
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	for _, args := range [][]string{
		{"serve", "--client-timeout", "30"},
		{"serve", "--client-timeout", "0"},
		{"serve", "--max-response-bytes", "1048576"},
		{"serve", "--client-timeout", "1.5", "--max-response-bytes", "65536"},
	} {
		if code, _, errOut := runCapture(t, args...); code != exitOK {
			t.Errorf("%v exit=%d, want %d (%q)", args, code, exitOK, errOut)
		}
	}
}

func TestRun_InvalidTimeoutAndSizeFlagsAreUsageErrors(t *testing.T) {
	// Negative timeout and a non-positive size are rejected before the client is
	// built (a 0 size would otherwise silently fall back to the 32 MiB default).
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"serve", "--client-timeout", "-1"}, "--client-timeout"},
		{[]string{"serve", "--max-response-bytes", "0"}, "--max-response-bytes"},
		{[]string{"serve", "--max-response-bytes", "-5"}, "--max-response-bytes"},
	} {
		code, _, errOut := runCapture(t, tc.args...)
		if code != exitUsage {
			t.Errorf("%v exit=%d, want %d", tc.args, code, exitUsage)
		}
		if !strings.Contains(errOut, tc.want) {
			t.Errorf("%v error gives no hint about %q: %q", tc.args, tc.want, errOut)
		}
	}
}

// writeTokenFile writes content to a fresh file with the given mode.
func writeTokenFile(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil { // WriteFile's mode is subject to the umask
		t.Fatal(err)
	}
	return path
}

// --token-file keeps the token out of the process list: the file's token,
// trimmed, builds the client and the loop serves until EOF.
func TestRun_TokenFileIsReadAndTrimmed(t *testing.T) {
	t.Setenv("SDO_TOKEN", "")
	path := writeTokenFile(t, "  file-token-12345\n", 0o600)
	if got, err := readTokenFile(path); err != nil || got != "file-token-12345" {
		t.Fatalf("readTokenFile = %q, %v", got, err)
	}
	if code, _, errOut := runCapture(t, "serve", "--token-file", path); code != exitOK {
		t.Fatalf("serve --token-file exit=%d, want %d (%q)", code, exitOK, errOut)
	}
}

// A token file other users can read, an empty one, a missing one or a
// directory is refused (exit 3), and the file's content is never printed.
func TestRun_BadTokenFilesAreRefused(t *testing.T) {
	t.Setenv("SDO_TOKEN", "env-token-123456") // a bad --token-file must not fall back to it
	cases := map[string]string{
		"empty":   writeTokenFile(t, " \n", 0o600),
		"missing": filepath.Join(t.TempDir(), "nope"),
		"dir":     t.TempDir(),
	}
	if runtime.GOOS != "windows" {
		cases["group-readable"] = writeTokenFile(t, "secret-token-777", 0o640)
		cases["world-readable"] = writeTokenFile(t, "secret-token-777", 0o604)
	}
	for name, path := range cases {
		code, _, errOut := runCapture(t, "serve", "--token-file", path)
		if code != exitAuth || !strings.Contains(errOut, "--token-file") {
			t.Errorf("%s: exit=%d, want %d with a --token-file error (%q)", name, code, exitAuth, errOut)
		}
		if strings.Contains(errOut, "secret-token-777") {
			t.Errorf("%s: token file content leaked: %q", name, errOut)
		}
	}
}

func TestRun_TokenAndTokenFileAreExclusive(t *testing.T) {
	path := writeTokenFile(t, "file-token-12345", 0o600)
	if code, _, _ := runCapture(t, "serve", "--token", "flag-token-9876", "--token-file", path); code != exitUsage {
		t.Errorf("--token with --token-file exit=%d, want %d", code, exitUsage)
	}
}

// An unknown flag is a usage error (exit 2) that names the flag, not a help
// request.
func TestRun_UnknownFlagIsAUsageError(t *testing.T) {
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	code, _, errOut := runCapture(t, "serve", "--bogus")
	if code != exitUsage || !strings.Contains(errOut, "-bogus") {
		t.Errorf("exit=%d, want %d naming the flag (%q)", code, exitUsage, errOut)
	}
}

// A token file larger than any token is the wrong file: refused (exit 3)
// without its content being printed.
func TestRun_OversizedTokenFileIsRefused(t *testing.T) {
	t.Setenv("SDO_TOKEN", "env-token-123456")
	path := writeTokenFile(t, "secret-token-777"+strings.Repeat("x", maxTokenFileBytes), 0o600)
	code, _, errOut := runCapture(t, "serve", "--token-file", path)
	if code != exitAuth || !strings.Contains(errOut, "is larger than 65536 bytes") {
		t.Errorf("exit=%d, want %d with a size error (%q)", code, exitAuth, errOut)
	}
	if strings.Contains(errOut, "secret-token-777") {
		t.Errorf("token file content leaked: %q", errOut)
	}
}

// A plain-http gateway on a non-loopback host is refused unless
// --insecure-http opts in. No request is made: stdin is already at EOF.
func TestRun_InsecureHTTPAllowsAPlainHTTPGateway(t *testing.T) {
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	t.Setenv("SDO_BASE_URL", "http://gateway.invalid")
	if code, _, errOut := runCapture(t, "serve"); code != exitFailure || !strings.Contains(errOut, "SDO_BASE_URL") {
		t.Errorf("without --insecure-http: exit=%d, want %d naming SDO_BASE_URL (%q)", code, exitFailure, errOut)
	}
	if code, _, errOut := runCapture(t, "serve", "--insecure-http"); code != exitOK {
		t.Errorf("with --insecure-http: exit=%d, want %d (%q)", code, exitOK, errOut)
	}
}

// A serve loop that fails (here: stdin cannot be read) exits 1 and says why.
func TestRun_ServeFailureExitsOne(t *testing.T) {
	t.Setenv("SDO_TOKEN", "valid-token-1234")
	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	inW.Close()
	inR.Close() // reads now fail instead of reaching EOF
	errF, err := os.CreateTemp(t.TempDir(), "err")
	if err != nil {
		t.Fatal(err)
	}
	defer errF.Close()
	code := run([]string{"serve"}, inR, os.Stdout, errF)
	errB, _ := os.ReadFile(errF.Name())
	if code != exitFailure || !strings.HasPrefix(string(errB), "sdo-mcp: ") || !strings.Contains(string(errB), "closed") {
		t.Errorf("exit=%d, want %d with the read error (%q)", code, exitFailure, errB)
	}
}

// main runs run with the process's arguments and exits with its code. It
// calls os.Exit, so it runs in a child process: the test binary itself, re-run
// with SDO_MCP_TEST_MAIN set to the arguments to give it.
func TestMainExitsWithRunsCode(t *testing.T) {
	if args, ok := os.LookupEnv("SDO_MCP_TEST_MAIN"); ok {
		os.Args = append([]string{"sdo-mcp"}, strings.Fields(args)...)
		main()
		return
	}
	for args, want := range map[string]int{"help": exitOK, "frobnicate": exitUsage} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainExitsWithRunsCode$")
		cmd.Env = append(os.Environ(), "SDO_MCP_TEST_MAIN="+args)
		out, err := cmd.CombinedOutput()
		code := 0
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("%s: %v", args, err)
		}
		if code != want || !strings.Contains(string(out), "Usage:") {
			t.Errorf("sdo-mcp %s: exit=%d, want %d (%q)", args, code, want, out)
		}
	}
}
