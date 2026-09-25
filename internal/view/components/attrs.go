package components

import (
	"strings"

	twmerge "github.com/Oudwins/tailwind-merge-go"
	"github.com/a-h/templ"
)

// withClass returns a copy of attrs whose "class" is base merged with the
// caller's class. Components spread the result instead of writing a literal
// class attribute: browsers keep the first of duplicate attributes, so a
// literal class would silently discard the caller's.
func withClass(base string, attrs templ.Attributes) templ.Attributes {
	out := make(templ.Attributes, len(attrs)+1)
	for k, v := range attrs {
		out[k] = v
	}
	out["class"] = base
	if extra, ok := attrs["class"].(string); ok && strings.TrimSpace(extra) != "" {
		out["class"] = mergeClasses(base + " " + extra)
	}
	return out
}

// mergeClasses drops utilities overridden later in the list (w-full then
// w-40): equal-specificity utilities resolve by stylesheet order, not
// attribute order, so without this the default wins about half the time.
// tailwind-merge-go decides which classes survive but returns them in map
// order, so the survivors are re-emitted in their original order.
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
