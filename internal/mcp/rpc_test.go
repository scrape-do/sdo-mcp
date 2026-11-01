// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func serveLines(t *testing.T, lines ...string) []map[string]any {
	t.Helper()
	var out strings.Builder
	if err := Serve(context.Background(), nil, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var replies []map[string]any
	dec := json.NewDecoder(strings.NewReader(out.String()))
	for dec.More() {
		var r map[string]any
		if err := dec.Decode(&r); err != nil {
			t.Fatalf("reply is not JSON: %v", err)
		}
		replies = append(replies, r)
	}
	return replies
}

// JSON-RPC 2.0 error handling: malformed JSON is -32700, a well-formed message
// that is not a valid request object is -32600, both with "id": null unless a
// valid id could be read; notifications never get a reply.
func TestJSONRPCErrorCodes(t *testing.T) {
	cases := []struct {
		line string
		code float64
		id   any
	}{
		{`{not json`, -32700, nil},
		{`[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, -32600, nil},
		{`[]`, -32600, nil},
		{`42`, -32600, nil},
		{`{"jsonrpc":"2.0","id":null,"method":"ping"}`, -32600, nil},
		{`{"jsonrpc":"2.0","id":{"x":1},"method":"ping"}`, -32600, nil},
		{`{"jsonrpc":"1.0","id":3,"method":"ping"}`, -32600, float64(3)},
		{`{"id":4,"method":"ping"}`, -32600, float64(4)},
		{`{"jsonrpc":"2.0","id":"a","method":7}`, -32600, "a"},
		{`{"jsonrpc":"2.0","id":5,"method":""}`, -32600, float64(5)},
		{`{"jsonrpc":"2.0","id":6,"method":"ping","params":"x"}`, -32600, float64(6)},
		{`{"jsonrpc":"2.0","id":-7,"method":"nope"}`, -32601, float64(-7)},
		{`{"jsonrpc":"2.0","method":"bad/version"}`, 0, nil}, // notification: no reply
	}
	for _, c := range cases {
		replies := serveLines(t, c.line)
		if c.code == 0 {
			if len(replies) != 0 {
				t.Errorf("%s: a notification got a reply: %v", c.line, replies)
			}
			continue
		}
		if len(replies) != 1 {
			t.Fatalf("%s: %d replies", c.line, len(replies))
		}
		r := replies[0]
		errObj, _ := r["error"].(map[string]any)
		id, hasID := r["id"]
		if errObj == nil || errObj["code"] != c.code || !hasID || id != c.id || r["jsonrpc"] != "2.0" {
			t.Errorf("%s: reply %v, want code %v id %v", c.line, r, c.code, c.id)
		}
	}
}

// A valid request with array params still reaches its handler.
func TestArrayParamsAreAccepted(t *testing.T) {
	replies := serveLines(t, `{"jsonrpc":"2.0","id":"p","method":"ping","params":[]}`)
	if len(replies) != 1 || replies[0]["result"] == nil {
		t.Errorf("replies = %v", replies)
	}
}
