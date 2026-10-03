package seed

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

var firstNames = []string{
	"Ada", "Alan", "Amara", "Anika", "Ariel", "Bao", "Beatriz", "Camille", "Chen", "Dara",
	"Diego", "Elif", "Emeka", "Farah", "Felix", "Grace", "Hana", "Ibrahim", "Ines", "Jonas",
	"Kai", "Kenji", "Lena", "Liam", "Lucia", "Malik", "Maya", "Mei", "Mateo", "Nadia",
	"Niko", "Noor", "Olga", "Omar", "Priya", "Quinn", "Rafael", "Rin", "Sami", "Sofia",
	"Tariq", "Thea", "Uma", "Viktor", "Wen", "Yara", "Yusuf", "Zane", "Zoe", "Ravi",
}

var lastNames = []string{
	"Abara", "Bianchi", "Castillo", "Dimitrov", "Eriksen", "Fischer", "Gupta", "Haddad", "Ito", "Jansen",
	"Kowalski", "Larsen", "Moreau", "Nakamura", "Okafor", "Petrov", "Quispe", "Rossi", "Silva", "Tanaka",
	"Umarov", "Varga", "Wagner", "Xu", "Yilmaz", "Zhang", "Mensah", "Novak", "Pereira", "Lindqvist",
}

var companies = []string{
	"", "", "", "Northwind", "Globex", "Initech", "Umbrella Labs", "Hooli", "Vandelay", "Stark Industries",
	"Wayne Tech", "Tyrell", "Soylent", "Aperture", "Cyberdyne", "freelance",
}

var locations = []string{
	"", "", "Berlin", "Lagos", "Tokyo", "São Paulo", "Toronto", "Bengaluru", "Lisbon", "Nairobi",
	"Seoul", "Mexico City", "Stockholm", "Melbourne", "Cairo", "Warsaw", "Austin", "Colombo",
}

var bios = []string{
	"", "Backend engineer. Mostly Go and Postgres.", "Building developer tools.", "Open source maintainer.",
	"Frontend, accessibility and design systems.", "SRE by day, tinkerer by night.", "Data pipelines and stream processing.",
	"Compilers, parsers and other small languages.", "Security researcher.", "Writing docs nobody reads (yet).",
	"Student. Learning Rust.", "Platform team lead.", "I break things so you don't have to.",
}

var orgNames = []string{
	"acme-labs", "northwind-dev", "lighthouse", "blue-harbor", "quantum-forge", "paper-planes",
	"open-meadow", "ironbark", "nimbus-works", "redwood-collective", "tidepool", "starling-io",
	"copperleaf", "polar-systems", "kestrel-hq",
}

var orgTaglines = []string{
	"Tools for teams that ship.", "Infrastructure you don't have to think about.", "Open source, built in the open.",
	"Data, maps and the glue in between.", "Small, sharp developer tools.", "The platform team.",
}

var repoAdjectives = []string{
	"swift", "tiny", "quiet", "bright", "lazy", "rapid", "solid", "hollow", "amber", "clever",
	"silent", "rusty", "crisp", "nimble", "mellow", "brave", "lucid", "plain", "sturdy", "frosty",
}

var repoNouns = []string{
	"ledger", "router", "parser", "cache", "beacon", "compass", "relay", "harbor", "kiln", "lantern",
	"meadow", "orbit", "pipeline", "quill", "sentry", "tracker", "vault", "widget", "atlas", "forge",
}

var repoPurposes = []string{
	"A small %s for %s workloads.", "Fast, dependency-free %s with a focus on %s.",
	"Experimental %s exploring %s.", "Production-ready %s used for %s.", "The %s behind our %s stack.",
}

var purposeTopics = []string{
	"cli", "http", "database", "devops", "testing", "parser", "api", "monitoring", "security",
	"machine-learning", "graphql", "kubernetes", "observability", "caching", "streaming",
}

var verbs = []string{
	"parse", "load", "build", "render", "fetch", "resolve", "encode", "decode", "validate", "merge",
	"compute", "format", "sync", "index", "scan", "flush", "apply", "normalize", "retry", "export",
}

var nouns = []string{
	"config", "user", "token", "cache", "request", "response", "session", "record", "event", "payload",
	"path", "header", "schema", "query", "batch", "report", "metric", "manifest", "chunk", "snapshot",
}

var issueTemplates = []string{
	"Crash when %s an empty %s", "%s ignores the %s timeout", "Add support for %s %s",
	"Docs: explain how to %s a %s", "%s fails on Windows when the %s has spaces", "Slow %s on large %s files",
	"Allow configuring how we %s the %s", "Panic in %s after %s reload", "Flaky test around %s %s",
	"Expose %s %s in the API",
}

var commentBodies = []string{
	"I can reproduce this on main.", "Thanks for the report! Looking into it.", "+1, we hit this in production last week.",
	"Could you share the full log output?", "I think this is related to the recent refactor.",
	"Fixed in the latest release, can you try again?", "This would be really useful for us too.",
	"Not sure this belongs in core — could it live in a plugin?", "Nice catch.", "LGTM once CI is green.",
	"Let's discuss this in the next sync.", "I've started on a fix, will open a PR soon.",
	"Workaround for now: set the env var and restart.", "Can we add a regression test for this?",
}

var reviewBodies = map[string][]string{
	"approved":          {"Looks good to me.", "LGTM, thanks!", "Nice cleanup.", "Ship it."},
	"changes_requested": {"A couple of things to fix before merging.", "Please add tests for the error path.", "This breaks the public API — can we keep the old name?"},
	"commented":         {"Left a few questions inline.", "Not blocking, just some thoughts.", "Interesting approach, want to understand the trade-off."},
}

var lineCommentBodies = []string{
	"Should this handle the empty case?", "Nit: naming.", "Can we extract this into a helper?",
	"Is this covered by a test?", "This allocates on every call — worth caching?", "Nice.",
}

// {repo} is replaced with the repository name; %s verbs go through fill.
var discussionTitles = map[string][]string{
	"General":       {"What are you building with {repo}?", "Roadmap thoughts for {repo}", "Show and tell: my {repo} setup"},
	"Q&A":           {"How do I %s a %s?", "Best way to %s %s in CI?", "Why does {repo} %s the %s twice?"},
	"Ideas":         {"Idea: built-in %s %s", "Proposal: %s %s by default", "Plugin API to %s the %s"},
	"Announcements": {"{repo} 1.0 is out", "Deprecating the old %s %s API", "Community call: how we %s the %s"},
}

var gistDescriptions = []string{
	"Handy %s snippet", "How I %s a %s", "Quick %s helper", "%s %s cheatsheet", "Benchmark: %s vs %s",
}

func pick[T any](rng *rand.Rand, xs []T) T { return xs[rng.IntN(len(xs))] }

func chance(rng *rand.Rand, p float64) bool { return rng.Float64() < p }

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// fill substitutes random verb/noun words into a template's %s verbs, alternating verb then noun.
func fill(rng *rand.Rand, tmpl string) string {
	n := strings.Count(tmpl, "%s")
	args := make([]any, n)
	for i := range args {
		if i%2 == 0 {
			args[i] = pick(rng, verbs)
		} else {
			args[i] = pick(rng, nouns)
		}
	}
	return capitalize(fmt.Sprintf(tmpl, args...))
}

func issueBody(rng *rand.Rand, lang *language) string {
	var b strings.Builder
	b.WriteString(pick(rng, []string{"Steps to reproduce:", "To reproduce:", "How to trigger it:"}))
	b.WriteString("\n\n")
	for i := range 2 + rng.IntN(3) {
		fmt.Fprintf(&b, "%d. %s the %s\n", i+1, capitalize(pick(rng, verbs)), pick(rng, nouns))
	}
	b.WriteString("\n**Expected:** it works.\n**Actual:** it doesn't.\n")
	if chance(rng, 0.5) {
		fmt.Fprintf(&b, "\n```%s\n%s```\n", lang.Fence, lang.block(rng, ident(rng)))
	}
	return b.String()
}

func commentBody(rng *rand.Rand, mention string) string {
	body := pick(rng, commentBodies)
	if mention != "" {
		body = "@" + mention + " " + body
	}
	return body
}
