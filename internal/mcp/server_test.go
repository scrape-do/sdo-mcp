// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdo "github.com/scrape-do/sdo-go"
)

func testClient(t *testing.T, handler http.HandlerFunc) *sdo.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := sdo.New("t", sdo.WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func req(method string, id any, params any) *Request {
	r := &Request{JSONRPC: "2.0", Method: method}
	if id != nil {
		b, _ := json.Marshal(id)
		r.ID = b
	}
	if params != nil {
		b, _ := json.Marshal(params)
		r.Params = b
	}
	return r
}

// The server echoes a protocol version it supports and otherwise offers its
// latest; serverInfo carries the CLI's version.
func TestInitializeNegotiatesTheProtocolVersion(t *testing.T) {
	cases := []struct{ ask, want string }{
		{"2024-11-05", "2024-11-05"},
		{"2025-03-26", "2025-03-26"},
		{"2025-06-18", "2025-06-18"},
		{"1999-01-01", latestProtocolVersion},
	}
	for _, c := range cases {
		params, want := map[string]any{"protocolVersion": c.ask}, c.want
		res := Handle(context.Background(), nil, req("initialize", 1, params)).Result.(map[string]any)
		if res["protocolVersion"] != want {
			t.Errorf("asked %v: protocolVersion = %v, want %s", params, res["protocolVersion"], want)
		}
		info := res["serverInfo"].(map[string]any)
		if info["version"] != serverVersion || info["name"] != serverName {
			t.Errorf("serverInfo = %v", info)
		}
	}
	res := Handle(context.Background(), nil, req("initialize", 1, nil)).Result.(map[string]any)
	if res["protocolVersion"] != latestProtocolVersion {
		t.Errorf("no params: protocolVersion = %v", res["protocolVersion"])
	}
}

func TestNotificationReturnsNil(t *testing.T) {
	if resp := Handle(context.Background(), nil, req("notifications/initialized", nil, nil)); resp != nil {
		t.Errorf("notification should yield no response, got %+v", resp)
	}
}

func TestToolsList(t *testing.T) {
	resp := Handle(context.Background(), nil, req("tools/list", 2, nil))
	tools := resp.Result.(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 2 || tools[0]["name"] != "scrape" || tools[1]["name"] != "scrape_plugin" {
		t.Errorf("tools = %v", tools)
	}
}

func TestUnknownMethod(t *testing.T) {
	resp := Handle(context.Background(), nil, req("does/not/exist", 3, nil))
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("expected a method not found (-32601) error, got %+v", resp.Error)
	}
}

func TestToolCallScrape(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Scrape.do-Request-Cost", "7")
		w.Write([]byte("<html>mcp body</html>"))
	})
	resp := Handle(context.Background(), client, req("tools/call", 4, map[string]any{
		"name":      "scrape",
		"arguments": map[string]any{"url": "https://example.com", "render": true},
	}))
	res := resp.Result.(map[string]any)
	if res["isError"].(bool) {
		t.Fatalf("unexpected error result: %v", res)
	}
	content := res["content"].([]map[string]any)
	meta, page := content[0]["text"].(string), content[1]["text"].(string)
	// Server metadata and page text are separate items, so page text cannot
	// pass itself off as the server's own lines.
	if !strings.Contains(meta, "status: 200") || !strings.Contains(meta, "cost: 7") || strings.Contains(meta, "mcp body") {
		t.Errorf("meta = %q", meta)
	}
	if !strings.Contains(page, "mcp body") || !strings.HasPrefix(page, "--- BEGIN UNTRUSTED CONTENT ") {
		t.Errorf("page = %q", page)
	}
}

func TestToolCallScrapeMissingURL(t *testing.T) {
	resp := Handle(context.Background(), nil, req("tools/call", 5, map[string]any{
		"name": "scrape", "arguments": map[string]any{},
	}))
	if resp.Error == nil || resp.Error.Code != codeInvalidParams ||
		!strings.Contains(resp.Error.Message, `missing required argument "url"`) {
		t.Errorf("want -32602 naming the missing url, got %+v", resp)
	}
}

func TestToolCallPlugin(t *testing.T) {
	client := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[1,2]}`))
	})
	resp := Handle(context.Background(), client, req("tools/call", 6, map[string]any{
		"name":      "scrape_plugin",
		"arguments": map[string]any{"route": "google/shopping", "params": map[string]any{"q": "x"}},
	}))
	text := resp.Result.(map[string]any)["content"].([]map[string]any)[1]["text"].(string)
	if !strings.Contains(text, `"results": [`) {
		t.Errorf("plugin text = %q", text)
	}
}

func TestServeRoundTrip(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" +
		`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
		`{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n")
	var out strings.Builder
	if err := Serve(context.Background(), nil, in, &out); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 { // initialize + ping; the notification yields no response
		t.Fatalf("expected 2 responses, got %d: %q", len(lines), out.String())
	}
}
