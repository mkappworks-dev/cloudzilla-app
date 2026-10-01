package components_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view/components"
)

var singleJSONString = regexp.MustCompile(`^"(?:[^"\\\n\r\x{2028}\x{2029}]|\\.)*"$`)

func TestJSLiteral_StringIsOneClosedLiteral(t *testing.T) {
	for _, s := range []string{
		`plain`,
		`',a:alert(1),b:'`,
		`trailing\`,
		`\',alert(1),'`,
		`"-alert(1)-"`,
		"line\nbreak sep ",
		`</script><script>alert(1)</script>`,
	} {
		got := components.JSLiteral(s)
		if !singleJSONString.MatchString(got) {
			t.Errorf("JSLiteral(%q) = %s, not a single closed string literal", s, got)
		}
		if strings.ContainsAny(got, "<>") {
			t.Errorf("JSLiteral(%q) = %s leaves raw angle brackets", s, got)
		}
		var back string
		if err := json.Unmarshal([]byte(got), &back); err != nil || back != s {
			t.Errorf("JSLiteral(%q) = %s decodes to %q (%v)", s, got, back, err)
		}
	}
}

func TestJSLiteral_ListAndMap(t *testing.T) {
	if got := components.JSLiteral([]string{`a\`, `',alert(1),'`}); got != `["a\\","',alert(1),'"]` {
		t.Errorf("list = %s", got)
	}
	if got := components.JSLiteral([]string{}); got != `[]` {
		t.Errorf("empty list = %s, want []", got)
	}
	if got := components.JSLiteral(map[string]string{"username": `x"}`}); got != `{"username":"x\"}"}` {
		t.Errorf("map = %s", got)
	}
}
