// SPDX-License-Identifier: Apache-2.0

package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"
)

// checkValue validates v, decoded with json.Decoder.UseNumber, against the
// subset of JSON Schema the tool schemas use: type (one name or a list of
// object, array, string, boolean, number, integer),
// properties, required, additionalProperties (false or a schema), items, enum
// and minLength. A model that sends "render": "true" or an argument the tool
// does not have gets told so, instead of a scrape that silently ignored it.
func checkValue(schema map[string]any, v any, path string) error {
	if types := schemaTypes(schema); len(types) > 0 && !slices.ContainsFunc(types, func(t string) bool { return isType(v, t) }) {
		return fmt.Errorf("%s must be %s, got %s", label(path), strings.Join(withArticles(types), " or "), describe(v))
	}
	s, _ := v.(string) // enum and minLength only ever sit on string schemas
	if enum, ok := schema["enum"].([]string); ok && !slices.Contains(enum, s) {
		return fmt.Errorf("%s must be one of %s, got %.40q", label(path), strings.Join(enum, ", "), s)
	}
	if n, ok := schema["minLength"].(int); ok && utf8.RuneCountInString(s) < n {
		return fmt.Errorf("%s must not be empty", label(path))
	}
	switch x := v.(type) {
	case map[string]any:
		return checkObject(schema, x, path)
	case []any:
		if items, ok := schema["items"].(map[string]any); ok {
			for i, e := range x {
				if err := checkValue(items, e, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkObject(schema, obj map[string]any, path string) error {
	props, _ := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]string)
	for _, r := range required {
		if _, ok := obj[r]; !ok {
			return fmt.Errorf("missing required %s", label(join(path, r)))
		}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		sub, known := props[k].(map[string]any)
		if !known {
			switch extra := schema["additionalProperties"].(type) {
			case map[string]any:
				sub = extra
			case bool:
				if !extra {
					return fmt.Errorf("unknown %s; accepted: %s", label(join(path, k)), strings.Join(sortedNames(props), ", "))
				}
			}
		}
		if sub != nil {
			if err := checkValue(sub, obj[k], join(path, k)); err != nil {
				return err
			}
		}
	}
	return nil
}

func schemaTypes(schema map[string]any) []string {
	switch t := schema["type"].(type) {
	case string:
		return []string{t}
	case []string:
		return t
	}
	return nil
}

func isType(v any, t string) bool {
	switch t {
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(json.Number)
		return ok
	case "integer":
		n, ok := v.(json.Number)
		return ok && integerRe.MatchString(string(n))
	}
	return false
}

// integerRe is a JSON number written as an integer: no fraction, no exponent.
var integerRe = regexp.MustCompile(`^-?[0-9]+$`)

func withArticles(types []string) []string {
	out := make([]string, len(types))
	for i, t := range types {
		out[i] = map[bool]string{true: "an ", false: "a "}[strings.ContainsRune("aeiou", rune(t[0]))] + t
	}
	return out
}

// describe names the JSON type of v for a message; a string's value is
// quoted, cut short.
func describe(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case string:
		return fmt.Sprintf("a string (%.40q)", x)
	case bool:
		return "a boolean"
	case json.Number:
		return "a number"
	case []any:
		return "an array"
	}
	return "an object"
}

// label names an argument for a message: the path, quoted and cut short (a
// model can send any key), or "arguments" for the whole object.
func label(path string) string {
	if path == "" {
		return "arguments"
	}
	return fmt.Sprintf("argument %.80q", path)
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func sortedNames(props map[string]any) []string {
	names := make([]string, 0, len(props))
	for k := range props {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}
