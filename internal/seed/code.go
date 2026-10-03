package seed

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

type identifier struct{ verb, noun string }

func ident(rng *rand.Rand) identifier { return identifier{pick(rng, verbs), pick(rng, nouns)} }

func (id identifier) camel() string  { return id.verb + capitalize(id.noun) }
func (id identifier) pascal() string { return capitalize(id.verb) + capitalize(id.noun) }
func (id identifier) snake() string  { return id.verb + "_" + id.noun }
func (id identifier) phrase() string { return id.verb + " " + id.noun }

type language struct {
	Name      string
	Ext       string
	Fence     string
	Dir       string
	Gitignore string
	header    string
	blocks    []func(id identifier) string
	manifest  func(module string, rng *rand.Rand) (path, content string)
}

func (l *language) block(rng *rand.Rand, id identifier) string { return pick(rng, l.blocks)(id) }

func (l *language) fileName(id identifier) string {
	stem := id.noun
	if l.Ext == ".go" || l.Ext == ".py" || l.Ext == ".rs" || l.Ext == ".rb" {
		stem = id.noun + "_" + id.verb
	}
	return l.Dir + "/" + stem + l.Ext
}

func pickDeps(rng *rand.Rand, all []string) []string {
	n := 1 + rng.IntN(len(all))
	out := make([]string, 0, n)
	for _, i := range rng.Perm(len(all))[:n] {
		out = append(out, all[i])
	}
	return out
}

var languages = []*language{
	{
		Name: "Go", Ext: ".go", Fence: "go", Dir: "internal/app", Gitignore: "/bin/\n*.test\ncoverage.out\n",
		header: "package app\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\n// %s %ss each item, failing on the first empty one.\nfunc %s(items []string) ([]string, error) {\n\tout := make([]string, 0, len(items))\n\tfor _, item := range items {\n\t\tif item == \"\" {\n\t\t\treturn nil, fmt.Errorf(\"%s: empty item\")\n\t\t}\n\t\tout = append(out, strings.TrimSpace(item))\n\t}\n\treturn out, nil\n}\n",
					id.pascal(), id.verb, id.pascal(), id.camel())
			},
			func(id identifier) string {
				return fmt.Sprintf("\ntype %sOptions struct {\n\tLimit   int\n\tVerbose bool\n}\n\nfunc %s(opts %sOptions) string {\n\tif opts.Limit <= 0 {\n\t\topts.Limit = 10\n\t}\n\treturn fmt.Sprintf(\"%s limit=%%d\", opts.Limit)\n}\n",
					id.pascal(), id.pascal(), id.pascal(), id.phrase())
			},
		},
		manifest: func(module string, rng *rand.Rand) (string, string) {
			deps := pickDeps(rng, []string{"github.com/spf13/cobra v1.8.1", "github.com/go-chi/chi/v5 v5.1.0", "github.com/jmoiron/sqlx v1.4.0", "golang.org/x/sync v0.8.0", "github.com/stretchr/testify v1.9.0"})
			return "go.mod", "module " + module + "\n\ngo 1.23\n\nrequire (\n\t" + strings.Join(deps, "\n\t") + "\n)\n"
		},
	},
	{
		Name: "Python", Ext: ".py", Fence: "python", Dir: "src", Gitignore: "__pycache__/\n*.pyc\n.venv/\n",
		header: "import logging\n\nlog = logging.getLogger(__name__)\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\ndef %s(items):\n    \"\"\"%s every item, skipping blanks.\"\"\"\n    result = []\n    for item in items:\n        if not item:\n            log.warning(\"%s: skipping empty item\")\n            continue\n        result.append(item.strip())\n    return result\n",
					id.snake(), capitalize(id.phrase()), id.snake())
			},
			func(id identifier) string {
				return fmt.Sprintf("\nclass %s:\n    def __init__(self, limit=10):\n        self.limit = limit\n\n    def run(self, values):\n        return values[: self.limit]\n",
					id.pascal())
			},
		},
		manifest: func(_ string, rng *rand.Rand) (string, string) {
			deps := pickDeps(rng, []string{"requests==2.32.3", "fastapi==0.115.0", "pydantic==2.9.2", "sqlalchemy==2.0.35", "pytest==8.3.3"})
			return "requirements.txt", strings.Join(deps, "\n") + "\n"
		},
	},
	{
		Name: "TypeScript", Ext: ".ts", Fence: "ts", Dir: "src", Gitignore: "node_modules/\ndist/\n",
		header: "import { strict as assert } from \"node:assert\";\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\nexport function %s(items: string[]): string[] {\n  assert(Array.isArray(items), \"%s expects an array\");\n  return items\n    .map((item) => item.trim())\n    .filter((item) => item.length > 0);\n}\n",
					id.camel(), id.camel())
			},
			func(id identifier) string {
				return fmt.Sprintf("\nexport interface %sOptions {\n  limit?: number;\n  verbose?: boolean;\n}\n\nexport const %s = (opts: %sOptions = {}): string =>\n  `%s limit=${opts.limit ?? 10}`;\n",
					id.pascal(), id.camel(), id.pascal(), id.phrase())
			},
		},
		manifest: func(module string, rng *rand.Rand) (string, string) {
			return "package.json", packageJSON(module, rng, []string{"\"zod\": \"^3.23.8\"", "\"express\": \"^4.21.0\"", "\"date-fns\": \"^4.1.0\"", "\"typescript\": \"^5.6.2\""})
		},
	},
	{
		Name: "JavaScript", Ext: ".js", Fence: "js", Dir: "lib", Gitignore: "node_modules/\ncoverage/\n",
		header: "\"use strict\";\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\nfunction %s(items) {\n  if (!Array.isArray(items)) throw new TypeError(\"%s expects an array\");\n  return items.map((item) => String(item).trim()).filter(Boolean);\n}\nmodule.exports.%s = %s;\n",
					id.camel(), id.camel(), id.camel(), id.camel())
			},
			func(id identifier) string {
				return fmt.Sprintf("\nasync function %s(url, { retries = 3 } = {}) {\n  for (let i = 0; i < retries; i++) {\n    const res = await fetch(url);\n    if (res.ok) return res.json();\n  }\n  throw new Error(\"%s failed\");\n}\nmodule.exports.%s = %s;\n",
					id.camel(), id.phrase(), id.camel(), id.camel())
			},
		},
		manifest: func(module string, rng *rand.Rand) (string, string) {
			return "package.json", packageJSON(module, rng, []string{"\"lodash\": \"^4.17.21\"", "\"axios\": \"^1.7.7\"", "\"commander\": \"^12.1.0\"", "\"jest\": \"^29.7.0\""})
		},
	},
	{
		Name: "Rust", Ext: ".rs", Fence: "rust", Dir: "src", Gitignore: "/target/\n",
		header: "use std::collections::HashMap;\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\n/// %s each item, dropping blanks.\npub fn %s(items: &[String]) -> Vec<String> {\n    items\n        .iter()\n        .map(|item| item.trim().to_string())\n        .filter(|item| !item.is_empty())\n        .collect()\n}\n",
					capitalize(id.phrase()), id.snake())
			},
			func(id identifier) string {
				return fmt.Sprintf("\npub fn %s(words: &[&str]) -> HashMap<String, usize> {\n    let mut counts = HashMap::new();\n    for w in words {\n        *counts.entry(w.to_string()).or_insert(0) += 1;\n    }\n    counts\n}\n",
					id.snake())
			},
		},
		manifest: func(module string, rng *rand.Rand) (string, string) {
			deps := pickDeps(rng, []string{"serde = \"1.0\"", "tokio = \"1.40\"", "clap = \"4.5\"", "anyhow = \"1.0\"", "regex = \"1.11\""})
			name := module[strings.LastIndex(module, "/")+1:]
			return "Cargo.toml", "[package]\nname = \"" + name + "\"\nversion = \"0.1.0\"\nedition = \"2021\"\n\n[dependencies]\n" + strings.Join(deps, "\n") + "\n"
		},
	},
	{
		Name: "Ruby", Ext: ".rb", Fence: "ruby", Dir: "lib", Gitignore: ".bundle/\nvendor/\n",
		header: "# frozen_string_literal: true\n",
		blocks: []func(identifier) string{
			func(id identifier) string {
				return fmt.Sprintf("\ndef %s(items)\n  items.map(&:strip).reject(&:empty?)\nend\n", id.snake())
			},
			func(id identifier) string {
				return fmt.Sprintf("\nclass %s\n  def initialize(limit: 10)\n    @limit = limit\n  end\n\n  def call(values)\n    values.first(@limit)\n  end\nend\n", id.pascal())
			},
		},
		manifest: func(string, *rand.Rand) (string, string) {
			return "Gemfile", "source \"https://rubygems.org\"\n\ngem \"rake\"\ngem \"rspec\"\n"
		},
	},
}

func packageJSON(module string, rng *rand.Rand, all []string) string {
	name := module[strings.LastIndex(module, "/")+1:]
	return "{\n  \"name\": \"" + name + "\",\n  \"version\": \"0.1.0\",\n  \"dependencies\": {\n    " +
		strings.Join(pickDeps(rng, all), ",\n    ") + "\n  }\n}\n"
}

// srcFile renders as header + blocks so an appended or removed block maps to known line numbers.
type srcFile struct {
	header string
	blocks []string
}

func (f *srcFile) render() string {
	var b strings.Builder
	b.WriteString(f.header)
	for _, blk := range f.blocks {
		b.WriteString(blk)
	}
	return b.String()
}

func lineCount(s string) int { return strings.Count(s, "\n") }
