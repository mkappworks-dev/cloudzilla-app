package highlight

import (
	"bytes"
	"os"
	"regexp"
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
		{NormalizeLight(PlainTheme), PlainTheme},
		{NormalizeDark(PlainTheme), PlainTheme},
	} {
		if tc.got != tc.want {
			t.Errorf("got %q, want %q", tc.got, tc.want)
		}
	}
}

func TestLightAndDarkThemesPartitionTheCatalogPlusPlain(t *testing.T) {
	light, dark := LightThemes(), DarkThemes()
	if len(light)+len(dark) != len(Themes)+2 {
		t.Fatalf("light %d + dark %d != %d themes + Plain in each", len(light), len(dark), len(Themes))
	}
	for _, list := range [][]Theme{light, dark} {
		if list[len(list)-1].ID != PlainTheme {
			t.Errorf("last option = %q, want Plain", list[len(list)-1].ID)
		}
	}
	for _, th := range light {
		if th.Dark {
			t.Errorf("LightThemes includes dark theme %q", th.ID)
		}
	}
}

func TestStylesheet_HasNoRulesForPlain(t *testing.T) {
	if strings.Contains(string(Stylesheet()), `"`+PlainTheme+`"`) {
		t.Error("stylesheet styles the plain theme, which must inherit the site's colours")
	}
}

func TestSwatch(t *testing.T) {
	for _, th := range append(LightThemes(), DarkThemes()...) {
		sw := Swatch(th.ID)
		if len(sw) < 2 {
			t.Errorf("Swatch(%q) = %q, want a background and at least one token colour", th.ID, sw)
		}
	}
	if got := Swatch("dracula"); got[0] != "#282a36" {
		t.Errorf("dracula swatch starts %q, want its background #282a36", got[0])
	}
	if got := Swatch("no-such-theme"); got != nil {
		t.Errorf("unknown theme swatch = %q, want nil", got)
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

// Typography's pre color is near-white in light mode, so an .hl rule without its
// own color leaves unspanned tokens illegible on a light theme's background.
func TestStylesheet_EveryHlRuleSetsAColor(t *testing.T) {
	colorDecl := regexp.MustCompile(`[{;\s]color:`)
	rules := 0
	for _, line := range strings.Split(string(Stylesheet()), "\n") {
		if !strings.Contains(line, " .hl {") {
			continue
		}
		rules++
		if !colorDecl.MatchString(line) {
			t.Errorf("rule has no color: %s", line)
		}
	}
	if rules != len(Themes) {
		t.Errorf("found %d .hl rules, want one per theme (%d)", rules, len(Themes))
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
