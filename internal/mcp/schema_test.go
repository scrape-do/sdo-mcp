// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
)

// Arguments that do not match the tool's input schema are a JSON-RPC -32602
// error that says what is wrong: a mistyped value is not coerced or dropped,
// an unknown argument is not ignored.
func TestToolArgumentsAreValidated(t *testing.T) {
	cases := []struct{ tool, args, want string }{
		{"scrape", `{"url":"https://e.com","render":"true"}`, `argument \"render\" must be a boolean, got a string (\"true\")`},
		{"scrape", `{"url":"https://e.com","waitSelector":"#x"}`, `unknown argument \"waitSelector\"; accepted: device, geoCode, output, render, returnJson, sessionId, super, url`},
		{"scrape", `{"url":""}`, `argument \"url\" must not be empty`},
		{"scrape", `{"url":5}`, `argument \"url\" must be a string, got a number`},
		{"scrape", `{"url":null}`, `must be a string, got null`},
		{"scrape", `{"url":true}`, `argument \"url\" must be a string, got a boolean`},
		{"scrape", `{"url":"https://e.com","device":"phone"}`, `must be one of desktop, mobile, tablet, got \"phone\"`},
		{"scrape", `{"url":"https://e.com","super":[true]}`, `got an array`},
		{"scrape", `[1]`, `arguments must be an object, got an array`},
		{"scrape", `{}`, `missing required argument \"url\"`},
		{"scrape_plugin", `null`, `missing required argument \"route\"`},
		{"scrape_plugin", `{"route":"google/search","params":"q=laptop"}`, `argument \"params\" must be an object, got a string`},
		{"scrape_plugin", `{"route":"google/search","params":{"q":{"a":1}}}`, `argument \"params.q\" must be a string or a number or a boolean, got an object`},
		{"scrape_plugin", `{"route":"google/hotels","params":{"hotel_class":["4","5"]}}`, `argument \"params.hotel_class\" must be a string or a number or a boolean, got an array`},
		{"scrape", `{"url":"https://e.com","sessionId":1.5}`, `argument \"sessionId\" must be an integer or a string, got a number`},
		{"scrape_plugin", `{"route":"google/search","params":{"flag":true}, "extra":1}`, `unknown argument \"extra\"; accepted: params, route`},
		{"scrape_plugin", `{"route":"google/search","params":{"TOKEN":"x"}}`, `may not contain the key \"TOKEN\"`},
		{"scrape_plugin", `{"route":"google/search","params":{"":"x"}}`, `may not contain the key \"\"`},
	}
	for _, c := range cases {
		got := call(t, nil, c.tool, c.args)
		if !strings.Contains(got, `"code":-32602`) || !strings.Contains(got, c.want) {
			t.Errorf("%s %s:\n got %s\nwant %s", c.tool, c.args, got, c.want)
		}
	}
}

// The validator's array support (type "array", items) and its open ends: a
// schema without a type takes any value, a type name it does not know
// matches nothing. The tool schemas use none of these today, so they are
// checked here directly.
func TestCheckValueArraysAndUntypedSchemas(t *testing.T) {
	tags := map[string]any{"type": "object", "properties": map[string]any{
		"tags": map[string]any{"type": "array", "items": map[string]any{"type": "string", "minLength": 1}},
		"any":  map[string]any{},
		"odd":  map[string]any{"type": "tuple"},
		"list": map[string]any{"type": "array"},
	}}
	cases := []struct{ args, want string }{
		{`{"tags":["a","b"]}`, ""},
		{`{"tags":[]}`, ""},
		{`{"tags":"a"}`, `argument "tags" must be an array, got a string ("a")`},
		{`{"tags":["a",1]}`, `argument "tags[1]" must be a string, got a number`},
		{`{"tags":["a",""]}`, `argument "tags[1]" must not be empty`},
		{`{"list":[1,{"x":null},[true]]}`, ""}, // no items schema: elements are not checked
		{`{"any":null}`, ""},
		{`{"any":{"x":[1]}}`, ""},
		{`{"odd":[1]}`, `argument "odd" must be a tuple, got an array`},
	}
	for _, c := range cases {
		dec := json.NewDecoder(strings.NewReader(c.args))
		dec.UseNumber()
		var v any
		if err := dec.Decode(&v); err != nil {
			t.Fatalf("%s: %v", c.args, err)
		}
		err := checkValue(tags, v, "")
		if c.want == "" && err != nil || c.want != "" && (err == nil || err.Error() != c.want) {
			t.Errorf("%s: got %v, want %q", c.args, err, c.want)
		}
	}
}

// Every scrape argument the schema advertises reaches the gateway, and the
// schema lists every argument the server reads (unknown ones are rejected
// above), so none is hidden.
func TestEveryScrapeArgumentReachesTheWire(t *testing.T) {
	var mu sync.Mutex
	var query url.Values
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		query = r.URL.Query()
		mu.Unlock()
		w.Write([]byte("ok"))
	})
	args := map[string]any{}
	for _, a := range scrapeArgs {
		switch {
		case a.enum != nil:
			args[a.name] = a.enum[1]
		case a.typ == "boolean":
			args[a.name] = true
		default:
			args[a.name] = "v-" + a.name
		}
	}
	args["url"] = "https://e.com/x"
	args["sessionId"] = 1234 // an integer, as a model sends it
	b, _ := json.Marshal(args)
	if _, isErr := items(t, call(t, c, "scrape", string(b))); isErr {
		t.Fatal("scrape failed")
	}
	want := map[string]string{"url": "https://e.com/x", "render": "true", "super": "true", "geoCode": "v-geoCode",
		"device": "mobile", "output": "markdown", "returnJSON": "true", "sessionId": "1234"}
	mu.Lock()
	defer mu.Unlock()
	for k, v := range want {
		if query.Get(k) != v {
			t.Errorf("%s = %q, want %q (query %v)", k, query.Get(k), v, query)
		}
	}
	if len(want) != len(scrapeArgs) {
		t.Errorf("the test covers %d arguments, the tool has %d", len(want), len(scrapeArgs))
	}
}

// A multi-value plugin parameter is one comma-separated string (the plugins
// read the first value of a repeated key only, so arrays are refused above).
func TestPluginParamsAreScalars(t *testing.T) {
	var mu sync.Mutex
	var raw string
	c := pageClient(t, "t", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		raw = r.URL.RawQuery
		mu.Unlock()
		w.Write([]byte("ok"))
	})
	call(t, c, "scrape_plugin", `{"route":"google/hotels","params":{"hotel_class":"4,5","n":2,"ok":false}}`)
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(raw, "hotel_class=4%2C5") || !strings.Contains(raw, "n=2") || !strings.Contains(raw, "ok=false") {
		t.Errorf("query = %s", raw)
	}
}

// credentialName is what no tool argument may be called: the credential is
// the server's, never the model's.
var credentialName = regexp.MustCompile(`(?i)token|key|secret|auth`)

// credentialProperties walks a schema, nested ones included (properties,
// items, additionalProperties), and returns the paths of properties named
// like a credential.
func credentialProperties(path string, schema map[string]any) []string {
	var found []string
	props, _ := schema["properties"].(map[string]any)
	for name, sub := range props {
		if credentialName.MatchString(name) {
			found = append(found, path+"."+name)
		}
		if m, ok := sub.(map[string]any); ok {
			found = append(found, credentialProperties(path+"."+name, m)...)
		}
	}
	for _, k := range []string{"items", "additionalProperties"} {
		if m, ok := schema[k].(map[string]any); ok {
			found = append(found, credentialProperties(path+"."+k, m)...)
		}
	}
	return found
}

// No tools/list input schema, nested ones included, has a property named
// like a credential. Every schema is also closed, so an argument it does not
// list cannot be read, and every description says a call costs credits.
func TestToolSchemasCarryNoCredentialField(t *testing.T) {
	resp := Handle(context.Background(), nil, req("tools/list", 1, nil))
	b, _ := json.Marshal(resp.Result)
	var listed struct {
		Tools []struct {
			Name        string         `json:"name"`
			Description string         `json:"description"`
			InputSchema map[string]any `json:"inputSchema"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &listed); err != nil || len(listed.Tools) != len(tools) {
		t.Fatalf("tools/list: %v %s", err, b)
	}
	for _, tool := range listed.Tools {
		if found := credentialProperties(tool.Name, tool.InputSchema); len(found) > 0 {
			t.Errorf("credential-like properties: %v", found)
		}
		if tool.InputSchema["additionalProperties"] != false {
			t.Errorf("%s: the input schema is not closed (additionalProperties: false)", tool.Name)
		}
		if !strings.Contains(tool.Description, "spends Scrape.do credits") {
			t.Errorf("%s: the description does not say that a call spends credits", tool.Name)
		}
	}
	// The walk reaches nested schemas.
	nested := map[string]any{"properties": map[string]any{"params": map[string]any{
		"additionalProperties": map[string]any{"items": map[string]any{
			"properties": map[string]any{"X-Api-Key": map[string]any{}}}},
		"properties": map[string]any{"auth": map[string]any{}}}}}
	if found := credentialProperties("t", nested); len(found) != 2 {
		t.Errorf("walker found %v, want the nested auth and X-Api-Key", found)
	}
}
