package components

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestStatStrip_RendersAllItems(t *testing.T) {
	items := []StatItem{
		{Label: "Repositories", Value: 12, Subtitle: "active"},
		{Label: "Pull Requests", Value: 5, Subtitle: "open"},
		{Label: "Issues", Value: 23, Subtitle: ""},
		{Label: "Stars", Value: 100, Subtitle: "total"},
	}
	var buf bytes.Buffer
	if err := StatStrip(items).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, it := range items {
		if !strings.Contains(out, it.Label) {
			t.Errorf("expected label %q in output", it.Label)
		}
		if !strings.Contains(out, ">"+intStr(it.Value)+"<") && !strings.Contains(out, intStr(it.Value)) {
			t.Errorf("expected value %d in output", it.Value)
		}
		if it.Subtitle != "" && !strings.Contains(out, it.Subtitle) {
			t.Errorf("expected subtitle %q in output", it.Subtitle)
		}
	}
}

func TestStatStrip_OmitsEmptySubtitle(t *testing.T) {
	items := []StatItem{
		{Label: "Only", Value: 1, Subtitle: ""},
	}
	var buf bytes.Buffer
	if err := StatStrip(items).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	// The text-muted-foreground/70 class is only used for subtitles.
	if strings.Contains(out, "text-muted-foreground/70") {
		t.Errorf("expected no subtitle dd when Subtitle is empty, got: %s", out)
	}
}

func intStr(n int) string {
	// avoid importing strconv twice and keep the helper local to tests
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}
