package main

import (
	"strings"
	"testing"
)

func TestStaticize_PinsYearAges(t *testing.T) {
	in := `<head><script>
// Theme bootstrap
</script></head><body><time datetime="2023-01-02">3y</time><time>Updated 2y ago</time><td>2y</td><p>over 5y</p></body>`
	out, err := staticize([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`<time datetime="2023-01-02">1y</time>`, "<time>Updated 1y ago</time>", "<td>2y</td>", "over 5y"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
