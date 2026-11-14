// SPDX-License-Identifier: Apache-2.0

package main

import (
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/scrape-do/sdo-mcp/internal/cmdline"
)

// main prints the surface, and nothing else. It runs in a child process (the
// test binary itself, re-run with APISURFACE_TEST_MAIN set) so its stdout can
// be read.
func TestMainPrintsTheSurface(t *testing.T) {
	if os.Getenv("APISURFACE_TEST_MAIN") == "1" {
		main()
		os.Exit(0) // before the test framework prints PASS: stdout is main's alone
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainPrintsTheSurface$")
	cmd.Env = append(os.Environ(), "APISURFACE_TEST_MAIN=1")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != render() {
		t.Errorf("main printed something else than render():\n%s", got)
	}
}

// The committed golden is what this generator prints; CI regenerates it too,
// but a stale golden should already fail `go test`.
func TestGoldenIsCurrent(t *testing.T) {
	want, err := os.ReadFile("../../api-surface.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got := render(); got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Errorf("api-surface.txt is stale; regenerate with: go run ./tools/apisurface > api-surface.txt")
	}
}

var lineRe = regexp.MustCompile(`^(install|command|flag|mcp-tool|mcp-param) \S+( \S+)*$`)

// Every line follows the grammar: one symbol, single spaces, sorted, no
// versions, nothing duplicated.
func TestGrammar(t *testing.T) {
	var lines []string
	for _, l := range strings.Split(strings.TrimSuffix(render(), "\n"), "\n") {
		if !strings.HasPrefix(l, "#") {
			lines = append(lines, l)
		}
	}
	if !sort.StringsAreSorted(lines) {
		t.Error("lines are not sorted")
	}
	seen := map[string]bool{}
	for _, l := range lines {
		if !lineRe.MatchString(l) || strings.Contains(l, "  ") {
			t.Errorf("malformed line %q", l)
		}
		if regexp.MustCompile(`\d+\.\d+\.\d+`).MatchString(l) {
			t.Errorf("version-like value in %q", l)
		}
		if seen[l] {
			t.Errorf("duplicate line %q", l)
		}
		seen[l] = true
	}
	for _, want := range []string{
		"install sdo-mcp github.com/scrape-do/sdo-mcp/cmd/sdo-mcp",
		"command sdo-mcp.serve",
		"command sdo-mcp.version",
		"flag sdo-mcp.serve --token-file string",
		"flag sdo-mcp.serve --client-timeout float64",
		"mcp-tool scrape",
		"mcp-param scrape url string [required]",
		"mcp-param scrape device string enum=desktop|mobile|tablet",
	} {
		if !seen[want] {
			t.Errorf("missing %q", want)
		}
	}
}

// A zero default is omitted and any other is listed; a flag.Value that is
// not a flag.Getter has no Go type to name.
func TestFlagDefaultsAndTypes(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.Int("retries", 3, "")
	fs.Bool("quiet", false, "")
	fs.Var(plainValue{}, "plain", "")
	got := strings.Join(commandLines("x", []string{"run"}, map[string]*flag.FlagSet{"run": fs}), "\n")
	for _, want := range []string{"command x\n", "command x.run\n", "flag x.run --retries int default=3", "flag x.run --quiet bool\n",
		"flag x.run --plain value"} {
		if !strings.Contains(got+"\n", want) {
			t.Errorf("lacks %q:\n%s", want, got)
		}
	}
	if got := render(); strings.Contains(got, "default=false") || strings.Contains(got, "default=0") {
		t.Error("zero defaults must be omitted")
	}
}

type plainValue struct{}

func (plainValue) String() string   { return "" }
func (plainValue) Set(string) error { return nil }

// The install path is a main package whose directory is the binary's name,
// so `go install <installPath>@latest` installs a binary named sdo-mcp.
func TestInstallPathBuildsTheSdoMcpBinary(t *testing.T) {
	dir := filepath.Join("..", "..", strings.TrimPrefix(installPath, "github.com/scrape-do/sdo-mcp/"))
	f, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "main.go"), nil, parser.PackageClauseOnly)
	if err != nil || f.Name.Name != "main" {
		t.Fatalf("%s is not a main package: %v", dir, err)
	}
	if got := path.Base(installPath); got != cmdline.Name {
		t.Errorf("go install would name the binary %q, not %q", got, cmdline.Name)
	}
}
