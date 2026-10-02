package components

import (
	"maps"
	"strings"

	"github.com/Oudwins/tailwind-merge-go/pkg/twmerge"
	"github.com/a-h/templ"
)

// tailwind-merge-go v0.2.3 predates Tailwind v4 and files outline-hidden under
// outline colors, so it would drop it beside outline-ring.
var twMerge = func() twmerge.TwMergeFn {
	cfg := twmerge.MakeDefaultConfig()
	cfg.ClassGroups.NextPart["outline"].NextPart["hidden"] = twmerge.ClassPart{ClassGroupId: "outline-style"}
	return twmerge.CreateTwMerge(cfg, nil)
}()

// Components spread the result instead of writing a literal class attribute:
// browsers keep the first of duplicate attributes, so a literal class would
// silently discard the caller's.
func withClass(attrs templ.Attributes, base ...any) templ.Attributes {
	defaults := templ.Classes(base...).String()
	out := withDefaults(attrs, nil)
	out["class"] = defaults
	if extra, ok := attrs["class"].(string); ok && strings.TrimSpace(extra) != "" {
		out["class"] = mergeClasses(defaults + " " + extra)
	}
	return out
}

// Defaults can't be literal attributes ahead of a spread; see withClass.
func withDefaults(attrs, defaults templ.Attributes) templ.Attributes {
	out := make(templ.Attributes, len(defaults)+len(attrs))
	maps.Copy(out, defaults)
	maps.Copy(out, attrs)
	return out
}

// Equal-specificity utilities resolve by stylesheet order, not attribute
// order, so a default the caller overrides must be dropped, not just preceded.
// tailwind-merge-go returns the survivors in map order; re-emit them in the
// original order so rendered HTML stays stable.
func mergeClasses(classes string) string {
	survivors := map[string]bool{}
	for _, c := range strings.Fields(twMerge(classes)) {
		survivors[c] = true
	}
	kept := make([]string, 0, len(survivors))
	for _, c := range strings.Fields(classes) {
		if survivors[c] {
			kept = append(kept, c)
			delete(survivors, c)
		}
	}
	return strings.Join(kept, " ")
}
