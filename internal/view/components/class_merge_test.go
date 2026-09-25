package components

import (
	"bytes"
	"context"
	"maps"
	"regexp"
	"strings"
	"testing"

	"github.com/a-h/templ"
)

var classAttr = regexp.MustCompile(`\sclass="([^"]*)"`)

func classTokens(html string) map[string]int {
	counts := map[string]int{}
	for _, m := range classAttr.FindAllStringSubmatch(html, -1) {
		for _, c := range strings.Fields(m[1]) {
			counts[c]++
		}
	}
	return counts
}

var duplicateClassAttr = regexp.MustCompile(`<[^>]*\sclass="[^"]*"[^>]*\sclass="`)

// Browsers keep the first of duplicate attributes, so a caller's class only
// applies if the component folds it into its own class attribute.
func TestComponents_MergeCallerClass(t *testing.T) {
	type build func(attrs templ.Attributes) templ.Component
	cases := []struct {
		name         string
		render       build
		defaultToken string
	}{
		{"Accordion", func(a templ.Attributes) templ.Component { return Accordion(a) }, "divide-y"},
		{"AccordionItem", func(a templ.Attributes) templ.Component { return AccordionItem(a) }, "group"},
		{"Button", func(a templ.Attributes) templ.Component { return Button(ButtonOutline, ButtonSizeSM, a) }, "inline-flex"},
		{"LinkButton", func(a templ.Attributes) templ.Component { return LinkButton("/x", ButtonDefault, ButtonSizeDefault, a) }, "inline-flex"},
		{"Card", func(a templ.Attributes) templ.Component { return Card(a) }, "rounded-lg"},
		{"Checkbox", func(a templ.Attributes) templ.Component { return Checkbox(a) }, "accent-foreground"},
		{"Switch", func(a templ.Attributes) templ.Component { return Switch(a) }, "sr-only"},
		{"Radio", func(a templ.Attributes) templ.Component { return Radio(a) }, "accent-foreground"},
		{"CommandItemLink", func(a templ.Attributes) templ.Component { return CommandItem("/x", a) }, "gap-2.5"},
		{"CommandItemButton", func(a templ.Attributes) templ.Component { return CommandItem("", a) }, "gap-2.5"},
		{"CommentEditor", func(a templ.Attributes) templ.Component { return CommentEditor(a) }, "overflow-hidden"},
		{"DropdownMenuItem", func(a templ.Attributes) templ.Component { return DropdownMenuItem(a) }, "rounded-sm"},
		{"DropdownMenuLink", func(a templ.Attributes) templ.Component { return DropdownMenuLink("/x", a) }, "rounded-sm"},
		{"Input", func(a templ.Attributes) templ.Component { return Input(a) }, "h-9"},
		{"SearchInput", func(a templ.Attributes) templ.Component { return SearchInput(a, "") }, "pl-8"},
		{"Textarea", func(a templ.Attributes) templ.Component { return Textarea(a) }, "min-h-[80px]"},
		{"Label", func(a templ.Attributes) templ.Component { return Label("x", false, a) }, "leading-none"},
		{"RadioGroup", func(a templ.Attributes) templ.Component { return RadioGroup("x", a) }, "grid"},
		{"Select", func(a templ.Attributes) templ.Component { return Select(a) }, "appearance-none"},
		{"Skeleton", func(a templ.Attributes) templ.Component { return Skeleton(a) }, "animate-pulse"},
		{"Table", func(a templ.Attributes) templ.Component { return Table(a) }, "caption-bottom"},
		{"TableHead", func(a templ.Attributes) templ.Component { return TableHead(a) }, "h-10"},
		{"TableCell", func(a templ.Attributes) templ.Component { return TableCell(a) }, "align-middle"},
		{"TableRowHead", func(a templ.Attributes) templ.Component { return TableRowHead(a) }, "font-normal"},
		{"TableCaption", func(a templ.Attributes) templ.Component { return TableCaption(a) }, "mt-4"},
		{"Toggle", func(a templ.Attributes) templ.Component { return Toggle(ToggleDefault, ButtonSizeSM, a) }, "inline-flex"},
	}
	merged := func(token string) *regexp.Regexp {
		return regexp.MustCompile(`class="(?:[^"]*\s)?` + regexp.QuoteMeta(token) + `(?:\s[^"]*)?\szz-caller"`)
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs := templ.Attributes{"class": "zz-caller", "data-probe": "1"}
			var buf bytes.Buffer
			if err := tc.render(attrs).Render(context.Background(), &buf); err != nil {
				t.Fatalf("render: %v", err)
			}
			out := buf.String()
			if duplicateClassAttr.MatchString(out) {
				t.Errorf("element has two class attributes: %s", out)
			}
			if !merged(tc.defaultToken).MatchString(out) {
				t.Errorf("want one class attribute holding %q then %q, got %s", tc.defaultToken, "zz-caller", out)
			}
			if attrs["class"] != "zz-caller" {
				t.Errorf("caller's attrs mutated: class = %v", attrs["class"])
			}

			var plain bytes.Buffer
			if err := tc.render(nil).Render(context.Background(), &plain); err != nil {
				t.Fatalf("render without attrs: %v", err)
			}
			want := classTokens(plain.String())
			want["zz-caller"]++
			if got := classTokens(out); !maps.Equal(got, want) {
				t.Errorf("a non-conflicting caller class changed the defaults:\n got %v\nwant %v", got, want)
			}
		})
	}
}

func TestWithClass_NoCallerClass(t *testing.T) {
	got := withClass("a b", templ.Attributes{"id": "x"})
	if got["class"] != "a b" {
		t.Errorf("class = %q, want %q", got["class"], "a b")
	}
	if got["id"] != "x" {
		t.Errorf("id dropped: %v", got)
	}
	if got := withClass("a b", nil); got["class"] != "a b" {
		t.Errorf("nil attrs: class = %q, want %q", got["class"], "a b")
	}
}

// Equal-specificity utilities resolve by stylesheet order, not attribute
// order, so a caller's override only wins if the conflicting default is dropped.
func TestWithClass_CallerWinsConflicts(t *testing.T) {
	cases := []struct {
		base, caller string
		want, drop   []string
	}{
		{"flex h-9 w-full text-sm bg-transparent", "w-40 h-12 text-lg bg-muted", []string{"flex", "w-40", "h-12", "text-lg", "bg-muted"}, []string{"w-full", "h-9", "text-sm", "bg-transparent"}},
		{"border border-input text-sm hover:bg-accent", "text-destructive border-destructive/40", []string{"border", "text-sm", "text-destructive", "border-destructive/40", "hover:bg-accent"}, []string{"border-input"}},
		{"animate-pulse rounded-md bg-muted", "h-8 w-8 rounded-full", []string{"animate-pulse", "bg-muted", "h-8", "w-8", "rounded-full"}, []string{"rounded-md"}},
		{"p-4 align-middle [&:has([role=checkbox])]:pr-0", "px-4 py-2.5 w-8", []string{"p-4", "px-4", "py-2.5", "w-8", "[&:has([role=checkbox])]:pr-0"}, nil},
		{"h-8 px-3 text-[13px]", "text-xs", []string{"h-8", "px-3", "text-xs"}, []string{"text-[13px]"}},
	}
	for _, tc := range cases {
		got := strings.Fields(withClass(tc.base, templ.Attributes{"class": tc.caller})["class"].(string))
		has := map[string]bool{}
		for _, c := range got {
			has[c] = true
		}
		for _, w := range tc.want {
			if !has[w] {
				t.Errorf("withClass(%q, %q) = %q: missing %q", tc.base, tc.caller, got, w)
			}
		}
		for _, d := range tc.drop {
			if has[d] {
				t.Errorf("withClass(%q, %q) = %q: kept conflicting default %q", tc.base, tc.caller, got, d)
			}
		}
	}
}

// tailwind-merge-go builds its result from a Go map, so its class order is
// random; rendered HTML must not change between identical requests.
func TestWithClass_StableOrder(t *testing.T) {
	base := "rounded-md border border-border bg-card divide-y divide-border overflow-hidden w-full text-sm"
	want := "rounded-md border border-border bg-card divide-y divide-border overflow-hidden text-sm w-40 zz-caller"
	for i := 0; i < 50; i++ {
		if got := withClass(base, templ.Attributes{"class": "w-40 zz-caller"})["class"]; got != want {
			t.Fatalf("run %d: class = %q, want %q", i, got, want)
		}
	}
}

// Variant and size strings are merged with the base too, so a conflict
// between them would surface only once a caller passes a class.
func TestButtonVariants_CallerClassKeepsDefaults(t *testing.T) {
	variants := []ButtonVariant{ButtonDefault, ButtonOutline, ButtonGhost, ButtonSecondary, ButtonDestructive, ButtonDestructiveOutline, ButtonSuccess, ButtonLink}
	sizes := []ButtonSize{ButtonSizeSM, ButtonSizeDefault, ButtonSizeLG, ButtonSizeIcon}
	render := func(c templ.Component) map[string]int {
		var buf bytes.Buffer
		if err := c.Render(context.Background(), &buf); err != nil {
			t.Fatalf("render: %v", err)
		}
		return classTokens(buf.String())
	}
	check := func(name string, plain, merged templ.Component) {
		want := render(plain)
		want["zz-caller"]++
		if got := render(merged); !maps.Equal(got, want) {
			t.Errorf("%s: caller class changed the defaults:\n got %v\nwant %v", name, got, want)
		}
	}
	probe := templ.Attributes{"class": "zz-caller"}
	for _, v := range variants {
		for _, s := range sizes {
			check(string(v)+"/"+string(s), Button(v, s, nil), Button(v, s, probe))
			check("link "+string(v)+"/"+string(s), LinkButton("/x", v, s, nil), LinkButton("/x", v, s, probe))
		}
	}
	for _, v := range []ToggleVariant{ToggleDefault, ToggleOutline} {
		for _, s := range sizes {
			check("toggle "+string(v)+"/"+string(s), Toggle(v, s, nil), Toggle(v, s, probe))
		}
	}
}
