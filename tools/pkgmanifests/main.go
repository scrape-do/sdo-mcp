// SPDX-License-Identifier: Apache-2.0

// Command pkgmanifests writes the Homebrew formula and the Scoop manifest for
// one sdo-mcp release, from nothing but the version and the release's
// checksums.txt:
//
//	go run ./tools/pkgmanifests -version 0.3.0 -checksums dist/checksums.txt -out dist/pkgmanifests
//
// writes dist/pkgmanifests/Formula/sdo-mcp.rb (for the scrape-do/homebrew-tap
// repository) and dist/pkgmanifests/bucket/sdo-mcp.json (for
// scrape-do/scoop-bucket).
// The release build job runs it with no GitHub API access, so the files are
// fixed before anyone approves the release, and the publishing job only
// copies them, after checking them against the signed checksums.txt.
// GoReleaser's own brews/scoops features would write to the tap from the build
// itself, with a token, and leave the output untested.
//
// It fails, writing nothing, unless the version is X.Y.Z or X.Y.Z-rc.N and
// checksums.txt is well formed and lists every archive the files point at.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
)

// The project this copy of the tool describes: the sdo-cli repository holds
// the same tool with its own values here.
const (
	project     = "sdo-mcp"
	repo        = "sdo-mcp"
	class       = "SdoMcp"
	description = "Model Context Protocol server for the Scrape.do web scraping API"
	versionArgs = "version" // what the formula's test runs to see the version
	homepage    = "https://scrape.do"
	license     = "Apache-2.0"
)

// Where the files go under -out: the paths they have in the tap and bucket.
var (
	formulaPath  = filepath.Join("Formula", project+".rb")
	manifestPath = filepath.Join("bucket", project+".json")
)

// The release workflow accepts exactly these tags (vX.Y.Z, vX.Y.Z-rc.N).
var versionRE = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-rc\.[0-9]+)?$`)

// sha256sum's output: anything else in checksums.txt means it is not the
// file GoReleaser wrote.
var hashRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

func main() {
	os.Exit(run(os.Args[1:], os.Stderr))
}

// run returns the exit code: 0, 1 when the files cannot be made, 2 for a
// command-line mistake.
func run(args []string, stderr io.Writer) int {
	fs := flag.NewFlagSet("pkgmanifests", flag.ContinueOnError)
	fs.SetOutput(stderr)
	version := fs.String("version", "", "the release version, X.Y.Z or X.Y.Z-rc.N (the tag without its v)")
	checksums := fs.String("checksums", "dist/checksums.txt", "the release's checksums.txt")
	out := fs.String("out", "dist/pkgmanifests", "directory to write Formula/ and bucket/ into")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "pkgmanifests: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if err := generate(*version, *checksums, *out); err != nil {
		fmt.Fprintln(stderr, "pkgmanifests:", err)
		return 1
	}
	return 0
}

// release is everything the two files are made from.
type release struct {
	Version string
	hashes  map[string]string // archive file name -> sha256
}

func generate(version, checksums, out string) error {
	if version == "" {
		return errors.New("-version is required")
	}
	if !versionRE.MatchString(version) {
		return fmt.Errorf("version %q is neither X.Y.Z nor X.Y.Z-rc.N", version)
	}
	f, err := os.Open(checksums)
	if err != nil {
		return err
	}
	defer f.Close()
	hashes, err := parseChecksums(f)
	if err != nil {
		return fmt.Errorf("%s: %w", checksums, err)
	}
	r := release{Version: version, hashes: hashes}
	for _, a := range archives {
		if _, ok := hashes[r.archive(a)]; !ok {
			return fmt.Errorf("%s: checksums.txt has no entry for %s", checksums, r.archive(a))
		}
	}
	files := []struct {
		path string
		data []byte
	}{{formulaPath, r.formula()}, {manifestPath, r.manifest()}}
	for _, file := range files {
		path := filepath.Join(out, file.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, file.data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// parseChecksums reads "<sha256>  <file>" lines, the format of sha256sum
// and of GoReleaser's checksums.txt.
func parseChecksums(r io.Reader) (map[string]string, error) {
	hashes := map[string]string{}
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf(`line %d: want "<sha256>  <file>", got %q`, n, line)
		}
		hash, name := fields[0], fields[1]
		if !hashRE.MatchString(hash) {
			return nil, fmt.Errorf("line %d: hash for %s is not 64 lowercase hex characters", n, name)
		}
		if _, dup := hashes[name]; dup {
			return nil, fmt.Errorf("line %d: %s is listed twice", n, name)
		}
		hashes[name] = hash
	}
	return hashes, sc.Err()
}

// platform is one release archive.
type platform struct{ os, arch, ext string }

// archives are the files .goreleaser.yaml builds, in checksums.txt order.
var archives = []platform{
	{"darwin", "amd64", "tar.gz"},
	{"darwin", "arm64", "tar.gz"},
	{"linux", "amd64", "tar.gz"},
	{"linux", "arm64", "tar.gz"},
	{"windows", "amd64", "zip"},
	{"windows", "arm64", "zip"},
}

func (r release) archive(p platform) string {
	return fmt.Sprintf("%s_%s_%s_%s.%s", project, r.Version, p.os, p.arch, p.ext)
}

func (r release) downloadURL(name string) string {
	return fmt.Sprintf("https://github.com/scrape-do/%s/releases/download/v%s/%s", repo, r.Version, name)
}

// URL and SHA256 give the formula template one archive's url and hash.
func (r release) URL(goos, goarch string) string {
	return r.downloadURL(r.archive(platform{goos, goarch, "tar.gz"}))
}

func (r release) SHA256(goos, goarch string) string {
	return r.hashes[r.archive(platform{goos, goarch, "tar.gz"})]
}

// The formula, laid out as `brew style` wants it. livecheck follows GitHub's
// "latest release", which is never a release candidate: Homebrew users get
// finals only.
var formulaTemplate = template.Must(template.New("formula").Parse(`class ` + class + ` < Formula
  desc "` + description + `"
  homepage "` + homepage + `"
  license "` + license + `"

  livecheck do
    url :stable
    strategy :github_latest
  end
{{range $os := .OSes}}
  on_{{$os.Block}} do
    on_arm do
      url "{{$.URL $os.GOOS "arm64"}}"
      sha256 "{{$.SHA256 $os.GOOS "arm64"}}"
    end
    on_intel do
      url "{{$.URL $os.GOOS "amd64"}}"
      sha256 "{{$.SHA256 $os.GOOS "amd64"}}"
    end
  end
{{end}}
  def install
    bin.install "` + project + `"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/` + project + ` ` + versionArgs + `")
  end
end
`))

func (r release) formula() []byte {
	var b bytes.Buffer
	// The template and its data are fixed in this file, so a failure is a
	// bug the golden test catches, not an input error.
	if err := formulaTemplate.Execute(&b, struct {
		release
		OSes []struct{ Block, GOOS string }
	}{r, []struct{ Block, GOOS string }{{"macos", "darwin"}, {"linux", "linux"}}}); err != nil {
		panic(err)
	}
	return b.Bytes()
}

// The Scoop manifest. Struct fields keep the key order; autoupdate lets
// Scoop's checkver find a newer release on its own, taking the hashes from
// that release's checksums.txt.
type scoopManifest struct {
	Version      string               `json:"version"`
	Description  string               `json:"description"`
	Homepage     string               `json:"homepage"`
	License      string               `json:"license"`
	Architecture map[string]scoopArch `json:"architecture"`
	Bin          string               `json:"bin"`
	Checkver     struct {
		GitHub string `json:"github"`
	} `json:"checkver"`
	Autoupdate scoopAutoupdate `json:"autoupdate"`
}

type scoopArch struct {
	URL  string `json:"url"`
	Hash string `json:"hash,omitempty"`
}

type scoopAutoupdate struct {
	Architecture map[string]scoopArch `json:"architecture"`
	Hash         struct {
		URL string `json:"url"`
	} `json:"hash"`
}

// scoopArches maps Scoop's architecture names to GOARCH. encoding/json
// writes map keys sorted, and "64bit" < "arm64" is the order wanted.
var scoopArches = map[string]string{"64bit": "amd64", "arm64": "arm64"}

func (r release) manifest() []byte {
	m := scoopManifest{
		Version:      r.Version,
		Description:  description,
		Homepage:     homepage,
		License:      license,
		Architecture: map[string]scoopArch{},
		Bin:          project + ".exe",
	}
	m.Checkver.GitHub = "https://github.com/scrape-do/" + repo
	m.Autoupdate.Architecture = map[string]scoopArch{}
	// Scoop substitutes $version itself when it updates the manifest.
	next := release{Version: "$version"}
	for scoop, goarch := range scoopArches {
		name := r.archive(platform{"windows", goarch, "zip"})
		m.Architecture[scoop] = scoopArch{URL: r.downloadURL(name), Hash: r.hashes[name]}
		m.Autoupdate.Architecture[scoop] = scoopArch{URL: next.downloadURL(next.archive(platform{"windows", goarch, "zip"}))}
	}
	m.Autoupdate.Hash.URL = next.downloadURL("checksums.txt")
	// Only strings and maps of strings: Marshal cannot fail.
	b, _ := json.MarshalIndent(m, "", "  ")
	return append(b, '\n')
}
