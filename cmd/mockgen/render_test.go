package main

import (
	"strings"
	"testing"
)

func TestStaticize_PinsYearAges(t *testing.T) {
	in := `<head><script>
// Theme bootstrap
</script></head><body><time>3y</time><time>Updated 2y ago</time><p>v0.4.0 and 12y of notes</p></body>`
	out, err := staticize([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<time>1y</time>", "<time>Updated 1y ago</time>", "12y of notes"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
