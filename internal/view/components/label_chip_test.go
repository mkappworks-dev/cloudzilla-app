package components

import (
	"bytes"
	"context"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/seed"
)

// Every surface a chip sits on in dark mode: --background, --card, --popover, --muted, --accent.
var darkSurfaces = []rgb{{0x00, 0x00, 0x00}, {0x12, 0x12, 0x12}, {0x1a, 0x1a, 0x1a}, {0x1f, 0x1f, 0x1f}, labelDarkSurface}

func TestContrastRatio(t *testing.T) {
	for _, tc := range []struct {
		a, b rgb
		want float64
	}{
		{rgb{0, 0, 0}, rgb{0xff, 0xff, 0xff}, 21},
		{rgb{0x77, 0x77, 0x77}, rgb{0xff, 0xff, 0xff}, 4.48},
		{rgb{0xff, 0xff, 0xff}, rgb{0xff, 0xff, 0xff}, 1},
	} {
		if got := contrastRatio(tc.a, tc.b); math.Abs(got-tc.want) > 0.01 {
			t.Errorf("contrastRatio(%s, %s) = %.3f, want %.2f", tc.a, tc.b, got, tc.want)
		}
	}
}

func assertChipReadable(t *testing.T, hex string) {
	t.Helper()
	c, ok := parseLabelHex(hex)
	if !ok {
		t.Fatalf("%s: not a label colour", hex)
	}
	col := chipColors(c)
	if r := contrastRatio(col.fg, col.bg); r < 4.5 {
		t.Errorf("%s light: %s on %s is %.2f:1", hex, col.fg, col.bg, r)
	}
	for _, s := range darkSurfaces {
		bg := mix(s, col.darkBase, labelDarkBgAlpha/255.0)
		if r := contrastRatio(col.darkFg, bg); r < 4.5 {
			t.Errorf("%s dark on %s: %s on %s is %.2f:1", hex, s, col.darkFg, bg, r)
		}
	}
}

func TestLabelChip_SeedPaletteReadableInBothThemes(t *testing.T) {
	// #e5e5e5 is the colour the label API fills in when none is given.
	hexes := []string{"#e5e5e5"}
	for _, l := range seed.DefaultLabels {
		hexes = append(hexes, l.Color)
	}
	for _, hex := range hexes {
		assertChipReadable(t, hex)
	}
}

func TestLabelChip_EveryShortHexReadableInBothThemes(t *testing.T) {
	const digits = "0123456789abcdef"
	for _, r := range digits {
		for _, g := range digits {
			for _, b := range digits {
				assertChipReadable(t, "#"+string([]rune{r, g, b}))
			}
		}
	}
}

func TestLabelChip_KeepsReadableColoursAsIs(t *testing.T) {
	col := chipColors(rgb{0xa2, 0xee, 0xef})
	if col.bg != (rgb{0xa2, 0xee, 0xef}) || col.fg != labelInkDark {
		t.Errorf("enhancement light chip = %s on %s, want %s on #a2eeef", col.fg, col.bg, labelInkDark)
	}
	if col.darkFg != (rgb{0xa2, 0xee, 0xef}) {
		t.Errorf("enhancement dark text = %s, want the label colour", col.darkFg)
	}
}

// The dark surfaces above mirror tailwind/input.css; a lighter token there would break the dark-mode guarantee.
func TestLabelChip_DarkSurfacesMatchTheme(t *testing.T) {
	css, err := os.ReadFile("../../../tailwind/input.css")
	if err != nil {
		t.Fatal(err)
	}
	_, dark, ok := strings.Cut(string(css), "\n  .dark {")
	if !ok {
		t.Fatal("no .dark block in tailwind/input.css")
	}
	dark, _, _ = strings.Cut(dark, "}")
	tokens := regexp.MustCompile(`--(background|card|popover|muted|accent): 0 0% ([\d.]+)%;`).FindAllStringSubmatch(dark, -1)
	if len(tokens) != 5 {
		t.Fatalf("found %d of 5 grey surface tokens in .dark: %v", len(tokens), tokens)
	}
	for _, tok := range tokens {
		l, _ := strconv.ParseFloat(tok[2], 64)
		grey := uint8(math.Round(l / 100 * 255))
		if grey > labelDarkSurface.r {
			t.Errorf("--%s is %s%% (#%02x), lighter than labelDarkSurface %s", tok[1], tok[2], grey, labelDarkSurface)
		}
	}
}

func TestLabelChip_Render(t *testing.T) {
	render := func(name, hex string) string {
		var buf bytes.Buffer
		if err := LabelChip(name, hex).Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return buf.String()
	}

	out := render("bug", "#d73a4a")
	for _, want := range []string{">bug</span>", "--label-bg:#d73a4a;--label-fg:#ffffff;", "--label-dark-bg:#d73a4a2e;", "dark:text-(color:--label-dark-fg)"} {
		if !strings.Contains(out, want) {
			t.Errorf("chip missing %q: %s", want, out)
		}
	}
	if short := render("x", "#abc"); !strings.Contains(short, "--label-bg:#aabbcc;") {
		t.Errorf("#abc not expanded: %s", short)
	}
	for _, hex := range []string{"", "none", "red", "#12345", `#fff;background:url(x)`} {
		out := render("plain", hex)
		if strings.Contains(out, "style=") || !strings.Contains(out, "bg-muted") {
			t.Errorf("%q: want the neutral chip, got %s", hex, out)
		}
	}
	if out := render(`<b>`, "#fff"); strings.Contains(out, "<b>") {
		t.Errorf("name not escaped: %s", out)
	}
}
