// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, dir string, files map[string]string) string {
	t.Helper()
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func listing(pkgs ...pkg) string {
	var b strings.Builder
	for _, p := range pkgs {
		j, _ := json.Marshal(p)
		b.Write(j)
		b.WriteString("\n")
	}
	return b.String()
}

func TestCollectWritesEveryLicenseOnce(t *testing.T) {
	goroot := writeFiles(t, t.TempDir(), map[string]string{"LICENSE": "Go BSD", "PATENTS": "Go patents", "README.md": "no"})
	a := writeFiles(t, t.TempDir(), map[string]string{"LICENSE.txt": "A MIT\n\n", "NOTICE": "A notice"})
	b := writeFiles(t, t.TempDir(), map[string]string{"COPYING": "B BSD"})
	os.Mkdir(filepath.Join(b, "LICENSES"), 0o755) // a directory is not a license file
	self := writeFiles(t, t.TempDir(), map[string]string{"LICENSE": "SELF"})
	in := listing(
		pkg{ImportPath: "fmt", Standard: true},
		pkg{ImportPath: "example.com/b", Module: &module{Path: "example.com/b", Version: "v1.0.0", Dir: "/nowhere",
			Replace: &module{Path: "../b", Dir: b}}},
		pkg{ImportPath: "example.com/a/x", Module: &module{Path: "example.com/a", Version: "v2.1.0", Dir: a}},
		pkg{ImportPath: "example.com/a/y", Module: &module{Path: "example.com/a", Version: "v2.1.0", Dir: a}},
		pkg{ImportPath: "example.com/self/cmd/sdo", Module: &module{Path: "example.com/self", Dir: self}},
	)
	out, err := collect(strings.NewReader(in), goroot)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		"Go standard library and runtime — LICENSE", "Go BSD", "Go patents",
		"example.com/a v2.1.0 — LICENSE.txt", "A MIT\n", "example.com/a v2.1.0 — NOTICE", "A notice",
		"example.com/b — COPYING", "B BSD",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SELF") || strings.Contains(got, "README") || strings.Count(got, "A MIT") != 1 {
		t.Errorf("unexpected content:\n%s", got)
	}
	if strings.Index(got, "example.com/a") > strings.Index(got, "example.com/b") {
		t.Error("modules are not sorted")
	}
}

func TestCollectFailsWithoutALicense(t *testing.T) {
	bare := t.TempDir()
	in := listing(pkg{ImportPath: "x/y", Module: &module{Path: "x/y", Dir: bare}}, pkg{ImportPath: "self", Module: &module{Path: "self"}})
	if _, err := collect(strings.NewReader(in), bare); err == nil || !strings.Contains(err.Error(), "no LICENSE") {
		t.Errorf("err = %v", err)
	}
	in = listing(pkg{ImportPath: "x/y", Module: &module{Path: "x/y", Dir: filepath.Join(bare, "missing")}}, pkg{ImportPath: "self", Module: &module{Path: "self"}})
	if _, err := collect(strings.NewReader(in), bare); err == nil {
		t.Error("a missing module directory was not reported")
	}
	if _, err := collect(strings.NewReader(`{"ImportPath": 1}`), bare); err == nil {
		t.Error("a malformed listing was not reported")
	}
	if _, err := collect(strings.NewReader(listing(pkg{Standard: true})), bare); err == nil {
		t.Error("a GOROOT without a license was not reported")
	}
}

// A license file that cannot be read stops the build, naming the module.
func TestCollectFailsOnAnUnreadableLicense(t *testing.T) {
	if runtime.GOOS == "windows" || os.Getuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	dir := writeFiles(t, t.TempDir(), map[string]string{"LICENSE": "MIT"})
	if err := os.Chmod(filepath.Join(dir, "LICENSE"), 0); err != nil {
		t.Fatal(err)
	}
	in := listing(pkg{ImportPath: "x/y", Module: &module{Path: "x/y", Version: "v1.0.0", Dir: dir}}, pkg{ImportPath: "self", Module: &module{Path: "self"}})
	if _, err := collect(strings.NewReader(in), dir); err == nil || !strings.HasPrefix(err.Error(), "x/y v1.0.0: ") {
		t.Errorf("err = %v", err)
	}
}

// main writes the file -o names for the packages given, ./cmd/sdo-mcp by default,
// and exits 1 with the reason when that fails. It calls os.Exit, so it runs
// in a child process: the test binary itself, re-run with THIRDPARTY_TEST_MAIN
// set to the arguments to give it.
func TestMainWritesTheFileOrExitsOne(t *testing.T) {
	if args, ok := os.LookupEnv("THIRDPARTY_TEST_MAIN"); ok {
		os.Args = append([]string{"thirdparty"}, strings.Fields(args)...)
		main()
		return
	}
	runMain := func(dir string, args ...string) (int, string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestMainWritesTheFileOrExitsOne$")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "THIRDPARTY_TEST_MAIN="+strings.Join(args, " "))
		out, err := cmd.CombinedOutput()
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode(), string(out)
		} else if err != nil {
			t.Fatal(err)
		}
		return 0, string(out)
	}
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "licenses.txt")
	if code, msg := runMain(repo, "-o", out); code != 0 {
		t.Fatalf("default package from the repository root: exit %d (%s)", code, msg)
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), "github.com/scrape-do/sdo-go") {
		t.Errorf("-o file lacks the dependencies:\n%s", b)
	}
	// From this directory the default ./cmd/sdo-mcp does not exist.
	missing := filepath.Join(t.TempDir(), "never.txt")
	if code, msg := runMain(".", "-o", missing); code != 1 || !strings.Contains(msg, "thirdparty: go list") {
		t.Errorf("missing package: exit %d, want 1 with the reason (%s)", code, msg)
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("a failed run wrote the -o file")
	}
}

// End to end, on the real binary: the SDK and the server's dependencies are
// listed with their texts; the server itself is not.
func TestRunListsTheBinarysDependencies(t *testing.T) {
	out := filepath.Join(t.TempDir(), "THIRD_PARTY_LICENSES.txt")
	if err := run(out, []string{"github.com/scrape-do/sdo-mcp/cmd/sdo-mcp"}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	got := string(b)
	for _, want := range []string{"Go standard library and runtime", "github.com/scrape-do/sdo-go",
		"golang.org/x/text", "github.com/andybalholm/brotli"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %s", want)
		}
	}
	if strings.Contains(got, "github.com/scrape-do/sdo-mcp ") {
		t.Error("the server's own module is listed")
	}
	if err := run(out, []string{"./does-not-exist"}); err == nil {
		t.Error("a go list failure was not reported")
	}
}
