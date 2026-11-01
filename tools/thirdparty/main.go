// SPDX-License-Identifier: Apache-2.0

// Command thirdparty writes the license and notice texts of everything
// statically linked into the sdo-mcp binary — each dependency module and the Go
// standard library and runtime — to one file, which the release archives ship
// next to the binary. It reads `go list -deps -json` and the module cache, so
// it needs no network access and no tool beyond the Go toolchain:
//
//	go run ./tools/thirdparty -o THIRD_PARTY_LICENSES.txt ./cmd/sdo-mcp
//
// A dependency without a license file stops the release build.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// targets are the operating systems the release builds for. One file serves
// every archive, so it covers the union of their dependencies (cobra links
// mousetrap on Windows only, for one).
var targets = []string{"linux", "darwin", "windows"}

func main() {
	out := flag.String("o", "THIRD_PARTY_LICENSES.txt", "file to write")
	flag.Parse()
	pkgs := flag.Args()
	if len(pkgs) == 0 {
		pkgs = []string{"./cmd/sdo-mcp"}
	}
	if err := run(*out, pkgs); err != nil {
		fmt.Fprintln(os.Stderr, "thirdparty:", err)
		os.Exit(1)
	}
}

func run(out string, pkgs []string) error {
	var listing bytes.Buffer
	for _, goos := range targets {
		list := exec.Command("go", append([]string{"list", "-deps", "-json"}, pkgs...)...)
		list.Env = append(os.Environ(), "GOOS="+goos, "CGO_ENABLED=0")
		list.Stdout, list.Stderr = &listing, os.Stderr
		if err := list.Run(); err != nil {
			return fmt.Errorf("go list (GOOS=%s): %w", goos, err)
		}
	}
	goroot, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return fmt.Errorf("go env GOROOT: %w", err)
	}
	text, err := collect(&listing, strings.TrimSpace(string(goroot)))
	if err != nil {
		return err
	}
	return os.WriteFile(out, text, 0o644)
}

type module struct {
	Path, Version, Dir string
	Replace            *module
}

type pkg struct {
	ImportPath string
	Standard   bool
	Module     *module
}

// collect reads a `go list -deps -json` stream and returns the notices file.
// The listed packages come last in the stream; their own module is this
// project, whose LICENSE and NOTICE ship on their own, so it is left out.
func collect(listing io.Reader, goroot string) ([]byte, error) {
	dec := json.NewDecoder(listing)
	mods := map[string]*module{}
	usesStd, self := false, ""
	for dec.More() {
		var p pkg
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("reading go list output: %w", err)
		}
		switch {
		case p.Standard:
			usesStd = true
		case p.Module != nil:
			mods[p.Module.Path] = p.Module
			self = p.Module.Path
		}
	}
	delete(mods, self)
	paths := make([]string, 0, len(mods))
	for path := range mods {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	var b bytes.Buffer
	b.WriteString("Third-party software in the sdo-mcp binary\n\n" +
		"The sdo-mcp binary is statically linked with the Go standard library and runtime\n" +
		"and with the modules below. Their license and notice texts follow, verbatim.\n")
	if usesStd {
		if err := section(&b, "Go standard library and runtime", goroot); err != nil {
			return nil, err
		}
	}
	for _, path := range paths {
		m := mods[path]
		dir, version := m.Dir, m.Version
		if m.Replace != nil {
			dir, version = m.Replace.Dir, m.Replace.Version
		}
		title := path
		if version != "" {
			title += " " + version
		}
		if err := section(&b, title, dir); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// noticePrefixes are the file names that carry license terms or notices.
var noticePrefixes = []string{"LICENSE", "LICENCE", "COPYING", "NOTICE", "PATENTS"}

// section writes each license or notice file at the top of dir.
func section(b *bytes.Buffer, title, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("%s: %w", title, err)
	}
	found := false
	for _, e := range entries {
		name := strings.ToUpper(e.Name())
		if !e.Type().IsRegular() || !hasAnyPrefix(name, noticePrefixes) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return fmt.Errorf("%s: %w", title, err)
		}
		fmt.Fprintf(b, "\n%s\n%s — %s\n%s\n\n", strings.Repeat("=", 78), title, e.Name(), strings.Repeat("=", 78))
		b.Write(bytes.TrimRight(text, "\n"))
		b.WriteString("\n")
		found = true
	}
	if !found {
		return errors.New(title + ": no LICENSE, COPYING or NOTICE file in " + dir)
	}
	return nil
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
