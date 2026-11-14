// SPDX-License-Identifier: Apache-2.0

package cmdline

import (
	"bytes"
	"strings"
	"testing"
)

// The serve flags parse into Serve, and their errors and help go to out.
func TestNewServeFlags(t *testing.T) {
	var out bytes.Buffer
	fs, s := NewServeFlags(&out)
	err := fs.Parse([]string{"--token", "t", "--token-file", "f", "--insecure-http",
		"--client-timeout", "1.5", "--max-response-bytes", "42"})
	if err != nil {
		t.Fatal(err)
	}
	if *s != (Serve{Token: "t", TokenFile: "f", InsecureHTTP: true, ClientTimeout: 1.5, MaxResponseBytes: 42}) {
		t.Errorf("parsed %+v", *s)
	}
	if fs.Name() != "sdo-mcp serve" {
		t.Errorf("flag set name %q", fs.Name())
	}
	if err := fs.Parse([]string{"--nope"}); err == nil || !strings.Contains(out.String(), "not defined: -nope") {
		t.Errorf("unknown flag: err=%v out=%q", err, out.String())
	}
}
