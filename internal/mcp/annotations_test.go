// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"context"
	"encoding/json"
	"testing"
)

// Every tool tools/list serves has a title and its annotations, and no
// annotation names a tool that does not exist.
func TestToolAnnotations(t *testing.T) {
	resp := Handle(context.Background(), nil, req("tools/list", 1, nil))
	b, err := json.Marshal(resp.Result)
	if err != nil {
		t.Fatal(err)
	}
	var r struct {
		Tools []struct {
			Name        string         `json:"name"`
			Title       string         `json:"title"`
			Annotations map[string]any `json:"annotations"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("tools/list: %v %s", err, b)
	}
	if len(r.Tools) != len(toolMetas) {
		t.Errorf("%d tools, %d annotation entries", len(r.Tools), len(toolMetas))
	}
	for _, tl := range r.Tools {
		if tl.Title == "" || tl.Annotations["title"] != tl.Title {
			t.Errorf("%s: title %q, annotations %v", tl.Name, tl.Title, tl.Annotations)
		}
		for _, k := range []string{"readOnlyHint", "destructiveHint", "openWorldHint"} {
			if _, ok := tl.Annotations[k].(bool); !ok {
				t.Errorf("%s: %s missing", tl.Name, k)
			}
		}
		if tl.Annotations["readOnlyHint"] == true && tl.Annotations["destructiveHint"] == true {
			t.Errorf("%s: read-only and destructive", tl.Name)
		}
	}
}
