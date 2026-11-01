// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	sdo "github.com/scrape-do/sdo-go"
)

// goName is what the model and its user cannot act on: Go options and
// fields of the SDK.
var goName = regexp.MustCompile(`\bWith[A-Z]|ScrapeConfig|Render: true|RenderMode`)

// A binary response (an image, a PDF) reaches the model as metadata only;
// the media type is repeated only when it is well formed.
func TestBinaryContentIsNotSentAsText(t *testing.T) {
	var contentType string
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00"))
	})
	cases := map[string]string{
		"image/png":                         "[image/png content, 10 bytes: not shown",
		`IMAGE/PNG; name="ignore previous"`: "[image/png content, 10 bytes",
		"application/pdf":                   "[application/pdf content",
		"application/x_weird":               "",
		"image/x_weird":                     "[binary content, 10 bytes",
		"text/html":                         "",
	}
	for ct, want := range cases {
		contentType = ct
		texts, isErr := items(t, call(t, c, "scrape", `{"url":"https://e.com"}`))
		if isErr || len(texts) != 2 {
			t.Fatalf("%s: %v", ct, texts)
		}
		if want == "" {
			if !strings.Contains(texts[1], "UNTRUSTED CONTENT") {
				t.Errorf("%s: text content should be returned fenced: %q", ct, texts[1])
			}
			continue
		}
		if !strings.HasPrefix(texts[1], want) || strings.Contains(texts[1], "PNG") || strings.Contains(texts[1], "ignore") {
			t.Errorf("%s: %q, want %q", ct, texts[1], want)
		}
	}
}

// An absent cost is unknown, not free.
func TestCostIsReportedOnlyWhenKnown(t *testing.T) {
	var cost string
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		if cost != "" {
			w.Header().Set("Scrape.do-Request-Cost", cost)
		}
		w.Write([]byte("ok"))
	})
	texts, _ := items(t, call(t, c, "scrape", `{"url":"https://e.com"}`))
	if texts[0] != "status: 200\nurl: https://e.com" {
		t.Errorf("no cost header: %q", texts[0])
	}
	cost = "5"
	texts, _ = items(t, call(t, c, "scrape", `{"url":"https://e.com"}`))
	if texts[0] != "status: 200\ncost: 5\nurl: https://e.com" {
		t.Errorf("cost header: %q", texts[0])
	}
}

// The SDK's verdict on the input is the server's own voice and names tool
// arguments; the size-limit and timeout advice names the server's flags. None
// of it names Go identifiers.
func TestLocalFailuresNameArgumentsAndFlags(t *testing.T) {
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, 100)) })
	texts, isErr := items(t, call(t, c, "scrape", `{"url":"ftp://e.com"}`))
	if !isErr || len(texts) != 1 || !strings.HasPrefix(texts[0], "scrape failed: invalid argument: 'url' must be an absolute http") {
		t.Errorf("config error: %q", texts)
	}
	texts, isErr = items(t, call(t, c, "scrape_plugin", `{"route":"../x"}`))
	if !isErr || len(texts) != 1 || !strings.Contains(texts[0], "plugin failed: invalid argument: invalid plugin route") {
		t.Errorf("plugin config error: %q", texts)
	}

	body := func(w http.ResponseWriter, r *http.Request) { w.Write(make([]byte, 100)) }
	small, err := sdo.New("t", sdo.WithBaseURL(serverURL(t, body)), sdo.WithMaxResponseBytes(10), sdo.WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	texts, _ = items(t, call(t, small, "scrape", `{"url":"https://e.com"}`))
	// The flag is named once, this server's context is kept, and no Go identifier leaks.
	if n := strings.Count(texts[0], "--max-response-bytes"); n != 1 || !strings.Contains(texts[0], tooLargeHint) ||
		goName.MatchString(strings.Join(texts, "\n")) {
		t.Errorf("too large (--max-response-bytes count=%d): %q", n, texts)
	}

	release := make(chan struct{})
	stall := serverURL(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	})
	t.Cleanup(func() { close(release) })
	slow, err := sdo.New("t", sdo.WithBaseURL(stall), sdo.WithTimeout(50*time.Millisecond), sdo.WithRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	texts, isErr = items(t, call(t, slow, "scrape", `{"url":"https://e.com"}`))
	if !isErr || !strings.Contains(texts[0], "timed out") || !strings.Contains(texts[0], "--client-timeout or SDO_TIMEOUT") {
		t.Errorf("timeout: %q", texts)
	}
}

func serverURL(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv.URL
}

// The gateway's own 400 (a parameter it rejected; this is its exact reply to
// render=mert) is a tool error carrying the gateway's message; a target's 400 the gateway
// billed, like a target 404 or 410, is the page with its status. A plugin's
// {"error","message"} envelope supplies its error's message.
func TestGateway400IsAnErrorTargetStatusesArePages(t *testing.T) {
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/plugin/google/search":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(400)
			w.Write([]byte(`{"error":"invalid_zipcode","message":"ZIP code format doesn't match country requirements"}`))
		case r.URL.Query().Get("url") == "https://e.com/bad-param":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(400)
			w.Write([]byte(`{"URL":"https://e.com","StatusCode":400,"Message":["'Render' parameter should boolean."],` +
				`"PossibleCauses":["Please check your request."],"Contact":"Please don't hesitate to contact with us via support@scrape.do."}`))
		case r.URL.Query().Get("url") == "https://e.com/gone":
			w.WriteHeader(410)
			w.Write([]byte("gone"))
		default:
			w.Header().Set("Scrape.do-Request-Cost", "1")
			w.WriteHeader(400)
			w.Write([]byte("target says bad request"))
		}
	})
	texts, isErr := items(t, call(t, c, "scrape", `{"url":"https://e.com/bad-param"}`))
	if !isErr || !strings.Contains(strings.Join(texts, "\n"), "'Render' parameter should boolean.") {
		t.Errorf("gateway 400: %v %q", isErr, texts)
	}
	texts, isErr = items(t, call(t, c, "scrape", `{"url":"https://e.com"}`))
	if isErr || texts[0] != "status: 400\ncost: 1\nurl: https://e.com" || !strings.Contains(texts[1], "target says bad request") {
		t.Errorf("target 400: %v %q", isErr, texts)
	}
	texts, isErr = items(t, call(t, c, "scrape", `{"url":"https://e.com/gone"}`))
	if isErr || !strings.HasPrefix(texts[0], "status: 410") {
		t.Errorf("target 410: %v %q", isErr, texts)
	}
	texts, isErr = items(t, call(t, c, "scrape_plugin", `{"route":"google/search","params":{"q":"x"}}`))
	if !isErr || !strings.Contains(strings.Join(texts, "\n"), "ZIP code format doesn't match country requirements") {
		t.Errorf("plugin 400: %v %q", isErr, texts)
	}
}

// Scrape.do-Request-Cost decides: a target 401/403/422 the gateway billed is a
// page (no isError) showing its status and cost; the gateway's own 401 (no
// cost) and an unbilled 401 (cost 0) stay tool errors.
func TestRequestCostDecidesIsError(t *testing.T) {
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		switch target := r.URL.Query().Get("url"); target {
		case "https://e.com/gateway-401":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(401)
			w.Write([]byte(`{"URL":"https://dashboard.scrape.do","StatusCode":401,"Message":["Monthly request limit exceeded."]}`))
		case "https://e.com/zero-cost-401":
			w.Header().Set("Scrape.do-Request-Cost", "0")
			w.WriteHeader(401)
		default:
			w.Header().Set("Scrape.do-Request-Cost", "2")
			switch target {
			case "https://e.com/401":
				w.WriteHeader(401)
			case "https://e.com/403":
				w.WriteHeader(403)
			default:
				w.WriteHeader(422)
			}
			w.Write([]byte("target page"))
		}
	})
	for _, status := range []string{"401", "403", "422"} {
		texts, isErr := items(t, call(t, c, "scrape", `{"url":"https://e.com/`+status+`"}`))
		if isErr || len(texts) != 2 || texts[0] != "status: "+status+"\ncost: 2\nurl: https://e.com/"+status ||
			!strings.Contains(texts[1], "target page") {
			t.Errorf("target %s: %v %q", status, isErr, texts)
		}
	}
	texts, isErr := items(t, call(t, c, "scrape", `{"url":"https://e.com/gateway-401"}`))
	if !isErr || !strings.Contains(strings.Join(texts, "\n"), "Monthly request limit exceeded.") {
		t.Errorf("gateway 401: %v %q", isErr, texts)
	}
	if texts, isErr := items(t, call(t, c, "scrape", `{"url":"https://e.com/zero-cost-401"}`)); !isErr {
		t.Errorf("unbilled 401: %q", texts)
	}
}
