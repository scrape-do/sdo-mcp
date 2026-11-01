// SPDX-License-Identifier: Apache-2.0

package explain

import (
	"regexp"
	"testing"
)

var cli = Names{
	Fields: map[string]string{
		"URL": "the target URL", "Render": "--render", "RenderMode": "--render-mode",
		"CustomWait": "--custom-wait", "Device": "--device", "JSON": "--data-json", "Data": "--data",
		"ExtraParams": "--param", "WaitSelector": "--wait-selector",
	},
	EnableRender: "pass --render",
}

// goIdentifier matches what must never reach a CLI or MCP user: a Go option,
// a qualified field, or the SDK's render advice.
var goIdentifier = regexp.MustCompile(`\bWith[A-Z]|ScrapeConfig|Request\.|Render: true|New\(token\)`)

func TestConfigNamesFlagsNotFields(t *testing.T) {
	cases := map[string]string{
		`ScrapeConfig.URL must be an absolute http or https URL with a host, got "example.com"`:        `the target URL must be an absolute http or https URL with a host, got "example.com"`,
		`CustomWait only applies to rendered requests: set Render: true`:                               `--custom-wait only applies to rendered requests: pass --render`,
		`Render is true but RenderMode is "false"; set only one of them`:                               `--render conflicts with --render-mode "false"; set only one of them`,
		`Device must be one of desktop|mobile|tablet (case-insensitive), got "Timeout"`:                `--device must be one of desktop|mobile|tablet (case-insensitive), got "Timeout"`,
		`Request.JSON and Request.Data are mutually exclusive`:                                         `--data-json and --data are mutually exclusive`,
		`ScrapeConfig requires a non-empty URL.`:                                                       `the target URL is required`,
		`ExtraParams may not set "url": the token comes from the client and url from ScrapeConfig.URL`: `--param may not set "url": the token comes from the client and url from the target URL`,
		`Unknown must stay`: `Unknown must stay`,
		`WaitSelector only applies to rendered requests: set Render to true`:                                  `--wait-selector only applies to rendered requests: set Render to true`,
		`a value for RenderMode, got "CustomWait \"x\" RenderMode"; see CustomWait`:                           `a value for --render-mode, got "CustomWait \"x\" RenderMode"; see --custom-wait`,
		`Request.JSON is not JSON-serializable: bad`:                                                          `--data-json is not JSON-serializable: bad`,
		`unterminated "RenderMode`:                                                                            `unterminated "RenderMode`,
		`a GET request cannot carry a body`:                                                                   `a GET request cannot carry a body`,
		`SDO_BASE_URL: http://x is plain http (http is allowed for localhost, or with WithAllowInsecureHTTP)`: `SDO_BASE_URL: http://x is plain http (http is allowed for localhost, or with --insecure-http)`,
	}
	for in, want := range cases {
		got := Config(in, cli)
		if got != want {
			t.Errorf("Config(%q)\n got %q\nwant %q", in, got, want)
		}
		if goIdentifier.MatchString(got) {
			t.Errorf("Config(%q) still names Go identifiers: %q", in, got)
		}
	}
}

func TestOptionsNamesFlagsAndEnv(t *testing.T) {
	in := "raise WithMaxResponseBytes; WithTimeout; WithBaseURL; WithAsyncBaseURL; Pass New(token)"
	want := "raise --max-response-bytes; --client-timeout; SDO_BASE_URL; SDO_ASYNC_BASE_URL; Pass --token"
	if got := Options(in); got != want {
		t.Errorf("Options = %q, want %q", got, want)
	}
}
