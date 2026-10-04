package highlight

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

func LightThemes() []Theme { return themesFor(false) }
func DarkThemes() []Theme  { return themesFor(true) }

func themesFor(dark bool) []Theme {
	var out []Theme
	for _, t := range Themes {
		if t.Dark == dark {
			out = append(out, t)
		}
	}
	return out
}

// NormalizeLight returns id when it is a light theme in the catalog, else the
// default, so a theme dropped from the catalog never needs a migration.
func NormalizeLight(id string) string { return normalize(id, false, DefaultLight) }

func NormalizeDark(id string) string { return normalize(id, true, DefaultDark) }

func normalize(id string, dark bool, fallback string) string {
	for _, t := range Themes {
		if t.ID == id && t.Dark == dark {
			return id
		}
	}
	return fallback
}
