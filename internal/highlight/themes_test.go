package highlight

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

func TestThemes_ExistInChroma(t *testing.T) {
	for _, th := range Themes {
		// styles.Get silently falls back to another style, so check the registry.
		if styles.Registry[th.ID] == nil {
			t.Errorf("theme %q is not a chroma style", th.ID)
		}
	}
}

func TestThemes_DarkFlagMatchesBackground(t *testing.T) {
	for _, th := range Themes {
		bg := styles.Registry[th.ID].Get(chroma.Background).Background
		if !bg.IsSet() {
			t.Errorf("theme %q has no background colour", th.ID)
			continue
		}
		if dark := bg.Brightness() < 0.5; dark != th.Dark {
			t.Errorf("theme %q: Dark = %v, but background %s says %v", th.ID, th.Dark, bg, dark)
		}
	}
}

func TestNormalize(t *testing.T) {
	for _, tc := range []struct {
		got, want string
	}{
		{NormalizeLight("solarized-light"), "solarized-light"},
		{NormalizeLight("dracula"), DefaultLight},
		{NormalizeLight(""), DefaultLight},
		{NormalizeDark("nord"), "nord"},
		{NormalizeDark("github"), DefaultDark},
		{NormalizeDark("retired-theme"), DefaultDark},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestLightAndDarkThemesPartitionTheCatalog(t *testing.T) {
	if len(LightThemes())+len(DarkThemes()) != len(Themes) {
		t.Fatalf("light %d + dark %d != %d themes", len(LightThemes()), len(DarkThemes()), len(Themes))
	}
	for _, th := range LightThemes() {
		if th.Dark {
			t.Errorf("LightThemes includes dark theme %q", th.ID)
		}
	}
}

func TestStylesheet_ScopesEachThemeToItsMode(t *testing.T) {
	css := string(Stylesheet())
	for _, want := range []string{
		`html:not(.dark)[data-code-light="github"] .hl {`,
		`html:not(.dark)[data-code-light="github"] .hl-k {`,
		`html.dark[data-code-dark="dracula"] .hl {`,
		`html.dark[data-code-dark="dracula"] .hl-cm {`,
	} {
		if !strings.Contains(css, want) {
			t.Errorf("stylesheet lacks %q", want)
		}
	}
	if strings.Contains(css, `.hl-w {`) || strings.Contains(css, `.hl-err {`) {
		t.Error("stylesheet styles whitespace or error tokens, which Lines never emits")
	}
}

func TestStylesheet_MatchesCommittedFile(t *testing.T) {
	committed, err := os.ReadFile("../../cmd/server/frontend/static/code-themes.css")
	if err != nil {
		t.Fatalf("read code-themes.css: %v", err)
	}
	if !bytes.Equal(committed, Stylesheet()) {
		t.Error("code-themes.css is stale; run make generate-code-themes")
	}
}
