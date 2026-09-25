package components

import (
	"strings"

	twmerge "github.com/Oudwins/tailwind-merge-go"
	"github.com/a-h/templ"
)

// Components spread the result instead of writing a literal class attribute:
// browsers keep the first of duplicate attributes, so a literal class would
// silently discard the caller's.
func withClass(attrs templ.Attributes, base ...any) templ.Attributes {
	defaults := templ.Classes(base...).String()
	out := make(templ.Attributes, len(attrs)+1)
	for k, v := range attrs {
		out[k] = v
	}
	out["class"] = defaults
	if extra, ok := attrs["class"].(string); ok && strings.TrimSpace(extra) != "" {
		out["class"] = mergeClasses(defaults + " " + extra)
	}
	return out
}

// Equal-specificity utilities resolve by stylesheet order, not attribute
// order, so a default the caller overrides must be dropped, not just preceded.
// tailwind-merge-go returns the survivors in map order; re-emit them in the
// original order so rendered HTML stays stable.
func mergeClasses(classes string) string {
	survivors := map[string]bool{}
	for _, c := range strings.Fields(twmerge.Merge(classes)) {
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
