package testutil

import (
	"encoding/json"
	"html"
	"regexp"
	"strings"
	"testing"
)

// HostileNames each break out of a hand-quoted JS string; every one carries "alert(" as its tell.
var HostileNames = []string{
	`',a:alert(1),b:'`,
	`\',alert(1),'`,
	`"-alert(1)-"`,
}

var (
	jsAttrRe     = regexp.MustCompile(`\s(x-data|x-init|x-show|x-text|x-html|x-effect|x-model|x-bind:[\w.:-]+|x-on:[\w.:-]+|@[\w.:-]+|:[\w.:-]+|hx-vals|hx-on[\w.:-]*)="([^"]*)"`)
	jsonStringRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"`)
)

// AssertNameStaysQuoted fails unless name reaches the rendered Alpine and htmx
// attributes only inside JSON string literals.
func AssertNameStaysQuoted(t *testing.T, rendered, name string) {
	t.Helper()
	reached := false
	for _, m := range jsAttrRe.FindAllStringSubmatch(rendered, -1) {
		expr := html.UnescapeString(m[2])
		code := jsonStringRe.ReplaceAllStringFunc(expr, func(lit string) string {
			var s string
			if json.Unmarshal([]byte(lit), &s) == nil && strings.Contains(s, name) {
				reached = true
			}
			return `""`
		})
		if strings.Contains(code, "alert(") {
			t.Errorf("%s evaluates %q as code: %s", m[1], name, expr)
		}
	}
	if !reached {
		t.Errorf("%q never reached an Alpine or htmx attribute as a JSON string", name)
	}
}
