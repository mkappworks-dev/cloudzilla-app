package highlight

import (
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/styles"
)

// Theme is a code theme users can pick; ID is its chroma style name.
type Theme struct {
	ID   string
	Name string
	Dark bool
}

var Themes = []Theme{
	{"github", "GitHub", false},
	{"solarized-light", "Solarized Light", false},
	{"catppuccin-latte", "Catppuccin Latte", false},
	{"gruvbox-light", "Gruvbox Light", false},
	{"tokyonight-day", "Tokyo Night Day", false},
	{"rose-pine-dawn", "Rosé Pine Dawn", false},
	{"github-dark", "GitHub Dark", true},
	{"onedark", "One Dark", true},
	{"dracula", "Dracula", true},
	{"monokai", "Monokai", true},
	{"nord", "Nord", true},
	{"solarized-dark", "Solarized Dark", true},
	{"catppuccin-mocha", "Catppuccin Mocha", true},
	{"tokyonight-night", "Tokyo Night", true},
	{"gruvbox", "Gruvbox", true},
}

const (
	DefaultLight = "github"
	DefaultDark  = "github-dark"
)

// PlainTheme leaves code in the site's own text and background colours. It is
// offered in both modes and has no rules in code-themes.css.
const PlainTheme = "plain"

func LightThemes() []Theme { return append(themesFor(false), Theme{PlainTheme, "Plain", false}) }
func DarkThemes() []Theme  { return append(themesFor(true), Theme{PlainTheme, "Plain", true}) }

// Swatch returns CSS colours that preview a theme: its background, then its
// keyword, string and function colours.
func Swatch(id string) []string {
	if id == PlainTheme {
		return []string{"hsl(var(--card))", "hsl(var(--foreground))"}
	}
	style := styles.Registry[id]
	if style == nil {
		return nil
	}
	bg := style.Get(chroma.Background)
	out := []string{bg.Background.String()}
	for _, tt := range []chroma.TokenType{chroma.Keyword, chroma.LiteralString, chroma.NameFunction} {
		if c := style.Get(tt).Colour; c.IsSet() {
			out = append(out, c.String())
		}
	}
	return out
}

func themesFor(dark bool) []Theme {
	var out []Theme
	for _, t := range Themes {
		if t.Dark == dark {
			out = append(out, t)
		}
	}
	return out
}

// NormalizeLight returns id when it is Plain or a light theme in the catalog, else the
// default, so a theme dropped from the catalog never needs a migration.
func NormalizeLight(id string) string { return normalize(id, false, DefaultLight) }

func NormalizeDark(id string) string { return normalize(id, true, DefaultDark) }

func normalize(id string, dark bool, fallback string) string {
	if id == PlainTheme {
		return id
	}
	for _, t := range Themes {
		if t.ID == id && t.Dark == dark {
			return id
		}
	}
	return fallback
}
