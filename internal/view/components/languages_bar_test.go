package components

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestLanguagesBar_RendersSegmentsAndLegend(t *testing.T) {
	items := []LangBarItem{
		{Name: "Go", Percent: 70, Color: "#00ADD8"},
		{Name: "Templ", Percent: 20, Color: "#66ADD0"},
		{Name: "CSS", Percent: 10, Color: "#563D7C"},
	}
	var buf bytes.Buffer
	if err := LanguagesBar(items).Render(context.Background(), &buf); err != nil {
		t.Fatalf("render: %v", err)
	}
	out := buf.String()
	for _, it := range items {
		if !strings.Contains(out, it.Name) {
			t.Errorf("expected name %q in output", it.Name)
		}
		pct := fmt.Sprintf("%d%%", it.Percent)
		if !strings.Contains(out, pct) {
			t.Errorf("expected percent %q in output", pct)
		}
		widthStyle := fmt.Sprintf("width: %d%%", it.Percent)
		if !strings.Contains(out, widthStyle) {
			t.Errorf("expected style with %q, got: %s", widthStyle, out)
		}
	}
	if !strings.Contains(out, `role="img"`) {
		t.Errorf("expected role=img a11y, got: %s", out)
	}
}

func TestLangColor_KnownAndUnknown(t *testing.T) {
	if got := LangColor("Go"); got != "#00ADD8" {
		t.Errorf("LangColor(Go) = %q, want #00ADD8", got)
	}
	if got := LangColor("XXX"); got != "#9CA3AF" {
		t.Errorf("LangColor(XXX) = %q, want #9CA3AF (fallback)", got)
	}
	if got := LangColor("Templ"); got != "#66ADD0" {
		t.Errorf("LangColor(Templ) = %q, want #66ADD0", got)
	}
}
