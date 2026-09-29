package components

import "encoding/json"

// The browser undoes templ's attribute escaping before Alpine or htmx evaluates
// an expression, so server values must arrive as JSON literals, never hand-quoted.
func JSLiteral(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	return string(b)
}
