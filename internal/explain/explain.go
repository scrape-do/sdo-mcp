// SPDX-License-Identifier: Apache-2.0

// Package explain rewrites the Go SDK's error text for the people who meet it
// through the sdo CLI or its MCP server. They set flags, environment variables
// and tool arguments, not Go struct fields and client options, so a message
// such as "CustomWait only applies to rendered requests: set Render: true" or
// "raise WithMaxResponseBytes" must name what they can actually change.
package explain

import (
	"regexp"
	"strings"
)

// options maps the SDK's client options to the flags and environment variables
// the operator sets on the sdo-mcp server — --max-response-bytes, --client-timeout
// (or SDO_TIMEOUT), --insecure-http, SDO_BASE_URL, SDO_ASYNC_BASE_URL and --token —
// the names the model can actually act on.
var options = strings.NewReplacer(
	"WithMaxResponseBytes", "--max-response-bytes",
	"WithAllowInsecureHTTP", "--insecure-http",
	"WithTimeout", "--client-timeout",
	"WithAsyncBaseURL", "SDO_ASYNC_BASE_URL",
	"WithBaseURL", "SDO_BASE_URL",
	"New(token)", "--token",
)

// Options renames the SDK's client options in any message: its own errors
// (a response over the size limit, a refused plain-http base URL) name the Go
// option that lifts the limit.
func Options(msg string) string { return options.Replace(msg) }

// Names says how the user names each SDK field.
type Names struct {
	// Fields maps a ScrapeConfig, Request or job field name (as the SDK
	// spells it in messages, e.g. "CustomWait", "URL", "Method") to the user's
	// name for it (e.g. "--custom-wait", "the target URL", "--method").
	Fields map[string]string
	// EnableRender tells the user how to turn rendering on, e.g.
	// "pass --render".
	EnableRender string
}

var (
	// qualified is a field named with its Go type ("ScrapeConfig.URL",
	// "Request.JSON").
	qualified = regexp.MustCompile(`\b(?:ScrapeConfig|Request)\.([A-Z][A-Za-z]*)\b`)
	// camel is a CamelCase field name ("RenderMode", "CustomWait"), which is
	// not an English word wherever it appears.
	camel = regexp.MustCompile(`\b[A-Z][a-z]+(?:[A-Z][A-Za-z]*)+\b`)
	// leading is a bare field name opening a message ("Device must be one
	// of ..."); elsewhere a capitalized word ("URL", "JSON") may be prose.
	leading = regexp.MustCompile(`^[A-Z][A-Za-z]*\b`)
)

const (
	renderHint     = `set Render: true`
	renderConflict = `Render is true but RenderMode is "false"; set only one of them`
	emptyURL       = "ScrapeConfig requires a non-empty URL."
)

// Config rewrites the text of a *sdo.ConfigError, which describes the input
// in the SDK's own terms, in n's terms. Quoted text (the user's own input,
// as in got "...") is left alone, and so are unknown names.
func Config(msg string, n Names) string {
	name := func(field string) string {
		if s, ok := n.Fields[field]; ok {
			return s
		}
		return field
	}
	switch msg {
	case emptyURL:
		return name("URL") + " is required"
	case renderConflict:
		return name("Render") + " conflicts with " + name("RenderMode") + ` "false"; set only one of them`
	}
	msg = strings.Replace(msg, renderHint, n.EnableRender, 1)
	msg = outsideQuotes(msg, func(part string) string {
		part = qualified.ReplaceAllStringFunc(part, func(m string) string {
			return name(m[strings.IndexByte(m, '.')+1:])
		})
		return camel.ReplaceAllStringFunc(part, name)
	})
	msg = leading.ReplaceAllStringFunc(msg, name)
	return Options(msg)
}

// outsideQuotes applies f to the parts of msg that are not inside a Go-quoted
// string ("...", with \" escapes).
func outsideQuotes(msg string, f func(string) string) string {
	var b strings.Builder
	start, quoted := 0, false
	for i := 0; i < len(msg); i++ {
		switch {
		case quoted && msg[i] == '\\':
			i++ // skip the escaped character
		case msg[i] == '"':
			if !quoted {
				b.WriteString(f(msg[start:i]))
				start = i
			} else {
				b.WriteString(msg[start : i+1])
				start = i + 1
			}
			quoted = !quoted
		}
	}
	if quoted {
		b.WriteString(msg[start:])
	} else {
		b.WriteString(f(msg[start:]))
	}
	return b.String()
}
