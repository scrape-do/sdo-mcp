// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The goldens are the files for the real v0.2.0 release, generated from its
// published checksums.txt; after an intended change to the output, rewrite
// them with: go test ./tools/pkgmanifests -update
var update = flag.Bool("update", false, "rewrite the golden files in testdata/")

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestGoldenFilesForTheV020Release(t *testing.T) {
	out := t.TempDir()
	var stderr bytes.Buffer
	if code := run([]string{"-version", "0.2.0", "-checksums", "testdata/checksums.txt", "-out", out}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	for _, name := range []string{formulaPath, manifestPath} {
		got := readFile(t, filepath.Join(out, name))
		golden := filepath.Join("testdata", name)
		if *update {
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		if want := readFile(t, golden); !bytes.Equal(got, want) {
			t.Errorf("%s differs from %s:\n%s", name, golden, got)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("unexpected stderr: %s", stderr.String())
	}
}

// The manifest must be plain JSON Scoop can read, with the keys in the
// documented order and the file ending in a newline.
func TestManifestIsOrderedJSON(t *testing.T) {
	b := readFile(t, filepath.Join("testdata", manifestPath))
	var v map[string]any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	keys := []string{`"version"`, `"description"`, `"homepage"`, `"license"`, `"architecture"`, `"bin"`, `"checkver"`, `"autoupdate"`}
	last := -1
	for _, k := range keys {
		i := bytes.Index(b, []byte("\n  "+k+":"))
		if i <= last {
			t.Errorf("key %s is missing or out of order", k)
		}
		last = i
	}
	if !bytes.HasSuffix(b, []byte("}\n")) {
		t.Error("the manifest does not end in a newline")
	}
}

// A release candidate gets files too (the release workflow lints them, and
// never publishes them), with the full version in every URL.
func TestReleaseCandidateVersion(t *testing.T) {
	sums := strings.ReplaceAll(string(readFile(t, "testdata/checksums.txt")), "0.2.0", "1.4.0-rc.2")
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(path, []byte(sums), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	var stderr bytes.Buffer
	if code := run([]string{"-version", "1.4.0-rc.2", "-checksums", path, "-out", out}, &stderr); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	formula := string(readFile(t, filepath.Join(out, formulaPath)))
	manifest := string(readFile(t, filepath.Join(out, manifestPath)))
	for _, want := range []string{"/v1.4.0-rc.2/sdo-mcp_1.4.0-rc.2_darwin_arm64.tar.gz", "/v1.4.0-rc.2/sdo-mcp_1.4.0-rc.2_linux_amd64.tar.gz"} {
		if !strings.Contains(formula, want) {
			t.Errorf("formula lacks %q", want)
		}
	}
	for _, want := range []string{`"version": "1.4.0-rc.2"`, "/v1.4.0-rc.2/sdo-mcp_1.4.0-rc.2_windows_arm64.zip"} {
		if !strings.Contains(manifest, want) {
			t.Errorf("manifest lacks %q", want)
		}
	}
	if strings.Contains(formula+manifest, "0.2.0") {
		t.Error("output mentions another version")
	}
}

func TestRejectsBadInput(t *testing.T) {
	good := string(readFile(t, "testdata/checksums.txt"))
	const darwinArm = "49470be4018df5e5713ae8cc6899d10f1d6e11235f2cc2b206cd1935c41da8a8  sdo-mcp_0.2.0_darwin_arm64.tar.gz\n"
	if !strings.Contains(good, darwinArm) {
		t.Fatal("testdata/checksums.txt changed; update this test")
	}
	cases := []struct {
		name, version, sums, want string
	}{
		{"no version", "", good, "-version is required"},
		{"leading v", "v0.2.0", good, `version "v0.2.0" is neither X.Y.Z nor X.Y.Z-rc.N`},
		{"two parts", "0.2", good, "is neither X.Y.Z nor X.Y.Z-rc.N"},
		{"other pre-release", "0.2.0-beta.1", good, "is neither X.Y.Z nor X.Y.Z-rc.N"},
		{"rc without number", "0.2.0-rc", good, "is neither X.Y.Z nor X.Y.Z-rc.N"},
		{"missing archive", "0.2.0", strings.Replace(good, darwinArm, "", 1),
			"checksums.txt has no entry for sdo-mcp_0.2.0_darwin_arm64.tar.gz"},
		{"other version's archives", "0.3.0", good, "checksums.txt has no entry for sdo-mcp_0.3.0_darwin_amd64.tar.gz"},
		{"short hash", "0.2.0", strings.Replace(good, darwinArm, darwinArm[1:], 1),
			"line 3: hash for sdo-mcp_0.2.0_darwin_arm64.tar.gz is not 64 lowercase hex characters"},
		{"uppercase hash", "0.2.0", strings.Replace(good, darwinArm, strings.ToUpper(darwinArm[:64])+darwinArm[64:], 1),
			"line 3: hash for sdo-mcp_0.2.0_darwin_arm64.tar.gz is not 64 lowercase hex characters"},
		{"non-hex hash", "0.2.0", strings.Replace(good, darwinArm, "g"+darwinArm[1:], 1), "is not 64 lowercase hex characters"},
		{"bad sbom hash too", "0.2.0", good + "xyz  sdo-mcp_0.2.0_extra.sbom.json\n", "line 13: hash for sdo-mcp_0.2.0_extra.sbom.json"},
		{"one field", "0.2.0", good + "lonely\n", "line 13: want \"<sha256>  <file>\""},
		{"three fields", "0.2.0", good + darwinArm[:64] + "  a b\n", "line 13: want \"<sha256>  <file>\""},
		{"conflicting entries", "0.2.0", good + strings.Repeat("0", 64) + "  sdo-mcp_0.2.0_darwin_arm64.tar.gz\n",
			"line 13: sdo-mcp_0.2.0_darwin_arm64.tar.gz is listed twice"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "checksums.txt")
			if err := os.WriteFile(path, []byte(c.sums), 0o644); err != nil {
				t.Fatal(err)
			}
			out := filepath.Join(dir, "out")
			var stderr bytes.Buffer
			code := run([]string{"-version", c.version, "-checksums", path, "-out", out}, &stderr)
			if code != 1 || !strings.HasPrefix(stderr.String(), "pkgmanifests: ") || !strings.Contains(stderr.String(), c.want) {
				t.Errorf("exit %d, stderr %q; want 1 and %q", code, stderr.String(), c.want)
			}
			if _, err := os.Stat(out); err == nil {
				t.Error("a failed run created the output directory")
			}
		})
	}
}

// A blank line (an editor's trailing newline) is not an entry.
func TestBlankLinesAreIgnored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "checksums.txt")
	if err := os.WriteFile(path, append([]byte("\n"), readFile(t, "testdata/checksums.txt")...), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := run([]string{"-version", "0.2.0", "-checksums", path, "-out", filepath.Join(dir, "out")}, &stderr); code != 0 {
		t.Errorf("exit %d: %s", code, stderr.String())
	}
}

func TestUsageAndIOErrors(t *testing.T) {
	var stderr bytes.Buffer
	if code := run([]string{"-nope"}, &stderr); code != 2 || !strings.Contains(stderr.String(), "-nope") {
		t.Errorf("unknown flag: exit %d, %q", code, stderr.String())
	}
	stderr.Reset()
	if code := run([]string{"-version", "0.2.0", "extra"}, &stderr); code != 2 || !strings.Contains(stderr.String(), "unexpected argument") {
		t.Errorf("extra argument: exit %d, %q", code, stderr.String())
	}
	stderr.Reset()
	missing := filepath.Join(t.TempDir(), "nope.txt")
	if code := run([]string{"-version", "0.2.0", "-checksums", missing}, &stderr); code != 1 || !strings.Contains(stderr.String(), "nope.txt") {
		t.Errorf("missing checksums: exit %d, %q", code, stderr.String())
	}

	// -out under a regular file cannot be created.
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := run([]string{"-version", "0.2.0", "-checksums", "testdata/checksums.txt", "-out", blocker}, &stderr); code != 1 || !strings.Contains(stderr.String(), "pkgmanifests: ") {
		t.Errorf("unwritable -out: exit %d, %q", code, stderr.String())
	}
	// The manifest's directory is taken by a file, after the formula was written.
	out := t.TempDir()
	if err := os.WriteFile(filepath.Join(out, "bucket"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := run([]string{"-version", "0.2.0", "-checksums", "testdata/checksums.txt", "-out", out}, &stderr); code != 1 || !strings.Contains(stderr.String(), "bucket") {
		t.Errorf("unwritable bucket/: exit %d, %q", code, stderr.String())
	}
	// The formula's own path is taken by a directory.
	out = t.TempDir()
	if err := os.MkdirAll(filepath.Join(out, formulaPath), 0o755); err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	if code := run([]string{"-version", "0.2.0", "-checksums", "testdata/checksums.txt", "-out", out}, &stderr); code != 1 || !strings.Contains(stderr.String(), formulaPath) {
		t.Errorf("formula path is a directory: exit %d, %q", code, stderr.String())
	}
}

// main exits with run's code, so a release build stops on bad input. It
// calls os.Exit, so it runs in a child process: the test binary itself,
// re-run with PKGMANIFESTS_TEST_MAIN set to the arguments to give it.
func TestMainExitsNonZeroOnBadInput(t *testing.T) {
	if args, ok := os.LookupEnv("PKGMANIFESTS_TEST_MAIN"); ok {
		os.Args = append([]string{"pkgmanifests"}, strings.Fields(args)...)
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainExitsNonZeroOnBadInput$")
	cmd.Env = append(os.Environ(), "PKGMANIFESTS_TEST_MAIN=-version 1.0 -checksums testdata/checksums.txt")
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || !strings.Contains(string(out), "is neither X.Y.Z") {
		t.Errorf("err = %v, output %q; want exit 1 naming the version", err, out)
	}
}
