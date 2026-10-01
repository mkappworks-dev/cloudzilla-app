package markdown_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/markdown"
)

type renderCase struct {
	name string
	src  string
	// Render's HTML reaches pages through templ.Raw, so a changed want must still
	// escape text and drop raw HTML and dangerous URLs.
	want string
}

func runRenderCases(t *testing.T, cases []renderCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := markdown.Render(tc.src); got != tc.want {
				t.Errorf("Render(%q)\n got: %q\nwant: %q", tc.src, got, tc.want)
			}
		})
	}
}

func TestRender_Blocks(t *testing.T) {
	runRenderCases(t, []renderCase{
		{
			name: "empty input",
			src:  "",
			want: "",
		},
		{
			name: "paragraph",
			src:  "Hello, world.",
			want: "<p>Hello, world.</p>\n",
		},
		{
			name: "soft line break becomes br",
			src: "line one\n" +
				"line two",
			want: "<p>line one<br>\n" +
				"line two</p>\n",
		},
		{
			name: "hard line breaks",
			src: "trailing spaces  \n" +
				"backslash\\\n" +
				"end",
			want: "<p>trailing spaces<br>\n" +
				"backslash<br>\n" +
				"end</p>\n",
		},
		{
			name: "atx headings",
			src: "# Title\n" +
				"## Sub heading\n" +
				"###### Six",
			want: "<h1 id=\"title\">Title</h1>\n" +
				"<h2 id=\"sub-heading\">Sub heading</h2>\n" +
				"<h6 id=\"six\">Six</h6>\n",
		},
		{
			name: "setext headings",
			src: "Title\n" +
				"=====\n" +
				"\n" +
				"Sub\n" +
				"---",
			want: "<h1 id=\"title\">Title</h1>\n" +
				"<h2 id=\"sub\">Sub</h2>\n",
		},
		{
			name: "duplicate heading ids",
			src: "# Intro\n" +
				"# Intro\n" +
				"## Intro",
			want: "<h1 id=\"intro\">Intro</h1>\n" +
				"<h1 id=\"intro-1\">Intro</h1>\n" +
				"<h2 id=\"intro-2\">Intro</h2>\n",
		},
		{
			name: "heading ids from punctuation and unicode",
			src: "# Hello, World! (v2)\n" +
				"## Café über naïve\n" +
				"## 日本語",
			want: "<h1 id=\"hello-world-v2\">Hello, World! (v2)</h1>\n" +
				"<h2 id=\"caf-ber-nave\">Café über naïve</h2>\n" +
				"<h2 id=\"heading\">日本語</h2>\n",
		},
		{
			name: "heading with inline markup",
			src:  "## *em* `code` [link](https://example.com) &amp; <b>raw</b>",
			want: "<h2 id=\"em-code-linkhttpsexamplecom-amp-brawb\"><em>em</em> <code>code</code> <a href=\"https://example.com\">link</a> &amp; <!-- raw HTML omitted -->raw<!-- raw HTML omitted --></h2>\n",
		},
		{
			name: "heading attribute syntax stays literal",
			src:  "## Heading {#custom .cls}",
			want: "<h2 id=\"heading-custom-cls\">Heading {#custom .cls}</h2>\n",
		},
		{
			name: "emphasis and strikethrough",
			src:  "*em* _em_ **strong** __strong__ ***both*** ~~strike~~ ~one~",
			want: "<p><em>em</em> <em>em</em> <strong>strong</strong> <strong>strong</strong> <em><strong>both</strong></em> <del>strike</del> <del>one</del></p>\n",
		},
		{
			name: "intraword emphasis",
			src:  "snake_case_name and foo*bar*baz",
			want: "<p>snake_case_name and foo<em>bar</em>baz</p>\n",
		},
		{
			name: "backslash escapes",
			src:  "\\*not em\\* \\# \\[x\\](y) \\<b\\> \\\\ \\a",
			want: "<p>*not em* # [x](y) &lt;b&gt; \\ \\a</p>\n",
		},
		{
			name: "entities and special characters",
			src:  "&copy; &amp; &#35; &#x41; &nosuch; AT&T 4 < 5 > 3 \"double\" 'single'",
			want: "<p>© &amp; # A &amp;nosuch; AT&amp;T 4 &lt; 5 &gt; 3 &quot;double&quot; 'single'</p>\n",
		},
		{
			name: "invalid and unterminated references",
			src:  "&#0; &#x110000; &#xD800; &#99999999; &copy &AMP; &amp",
			want: "<p>\uFFFD \uFFFD \uFFFD &amp;#99999999; &amp;copy &amp; &amp;amp</p>\n",
		},
		{
			name: "nul byte",
			src:  "a\x00b",
			want: "<p>a\uFFFDb</p>\n",
		},
		{
			name: "unordered list",
			src: "- one\n" +
				"- two\n" +
				"  - nested\n" +
				"- three",
			want: "<ul>\n" +
				"<li>one</li>\n" +
				"<li>two\n" +
				"<ul>\n" +
				"<li>nested</li>\n" +
				"</ul>\n" +
				"</li>\n" +
				"<li>three</li>\n" +
				"</ul>\n",
		},
		{
			name: "ordered list with start",
			src: "3. three\n" +
				"4. four",
			want: "<ol start=\"3\">\n" +
				"<li>three</li>\n" +
				"<li>four</li>\n" +
				"</ol>\n",
		},
		{
			name: "loose list",
			src: "- one\n" +
				"\n" +
				"- two",
			want: "<ul>\n" +
				"<li>\n" +
				"<p>one</p>\n" +
				"</li>\n" +
				"<li>\n" +
				"<p>two</p>\n" +
				"</li>\n" +
				"</ul>\n",
		},
		{
			name: "tight list items with continuation lines and blocks",
			src: "- one\n" +
				"  continued\n" +
				"- two\n" +
				"  > quote\n" +
				"- three",
			want: "<ul>\n" +
				"<li>one<br>\n" +
				"continued</li>\n" +
				"<li>two\n" +
				"<blockquote>\n" +
				"<p>quote</p>\n" +
				"</blockquote>\n" +
				"</li>\n" +
				"<li>three</li>\n" +
				"</ul>\n",
		},
		{
			name: "task lists",
			src: "- [ ] todo\n" +
				"- [x] done\n" +
				"- [X] upper\n" +
				"  - [ ] nested\n" +
				"\n" +
				"1. [x] first\n" +
				"2. [ ] second",
			want: "<ul>\n" +
				"<li><input disabled=\"\" type=\"checkbox\"> todo</li>\n" +
				"<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> done</li>\n" +
				"<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> upper\n" +
				"<ul>\n" +
				"<li><input disabled=\"\" type=\"checkbox\"> nested</li>\n" +
				"</ul>\n" +
				"</li>\n" +
				"</ul>\n" +
				"<ol>\n" +
				"<li><input checked=\"\" disabled=\"\" type=\"checkbox\"> first</li>\n" +
				"<li><input disabled=\"\" type=\"checkbox\"> second</li>\n" +
				"</ol>\n",
		},
		{
			name: "task syntax outside a list",
			src: "[ ] not a task\n" +
				"[x] nor this",
			want: "<p>[ ] not a task<br>\n" +
				"[x] nor this</p>\n",
		},
		{
			name: "blockquote",
			src: "> quote\n" +
				"> > nested\n" +
				">\n" +
				"> - item",
			want: "<blockquote>\n" +
				"<p>quote</p>\n" +
				"<blockquote>\n" +
				"<p>nested</p>\n" +
				"</blockquote>\n" +
				"<ul>\n" +
				"<li>item</li>\n" +
				"</ul>\n" +
				"</blockquote>\n",
		},
		{
			name: "thematic break",
			src: "above\n" +
				"\n" +
				"---\n" +
				"\n" +
				"below",
			want: "<p>above</p>\n" +
				"<hr>\n" +
				"<p>below</p>\n",
		},
		{
			name: "table with alignment",
			src: "| Left | Center | Right | None |\n" +
				"|:-----|:------:|------:|------|\n" +
				"| a | **b** | `c` | d \\| e |\n" +
				"| <b>raw</b> | [l](javascript:alert(1)) | | f |",
			want: "<table>\n" +
				"<thead>\n" +
				"<tr>\n" +
				"<th style=\"text-align:left\">Left</th>\n" +
				"<th style=\"text-align:center\">Center</th>\n" +
				"<th style=\"text-align:right\">Right</th>\n" +
				"<th>None</th>\n" +
				"</tr>\n" +
				"</thead>\n" +
				"<tbody>\n" +
				"<tr>\n" +
				"<td style=\"text-align:left\">a</td>\n" +
				"<td style=\"text-align:center\"><strong>b</strong></td>\n" +
				"<td style=\"text-align:right\"><code>c</code></td>\n" +
				"<td>d | e</td>\n" +
				"</tr>\n" +
				"<tr>\n" +
				"<td style=\"text-align:left\"><!-- raw HTML omitted -->raw<!-- raw HTML omitted --></td>\n" +
				"<td style=\"text-align:center\"><a href=\"#\">l</a></td>\n" +
				"<td style=\"text-align:right\"></td>\n" +
				"<td>f</td>\n" +
				"</tr>\n" +
				"</tbody>\n" +
				"</table>\n",
		},
		{
			name: "table without body rows",
			src: "| a | b |\n" +
				"|---|---|",
			want: "<table>\n" +
				"<thead>\n" +
				"<tr>\n" +
				"<th>a</th>\n" +
				"<th>b</th>\n" +
				"</tr>\n" +
				"</thead>\n" +
				"</table>\n",
		},
		{
			name: "footnote syntax is not enabled",
			src: "Text[^1]\n" +
				"\n" +
				"[^1]: https://example.com/note",
			want: "<p>Text<a href=\"https://example.com/note\">^1</a></p>\n",
		},
	})
}

func TestRender_Links(t *testing.T) {
	runRenderCases(t, []renderCase{
		{
			name: "inline link with title",
			src:  "[text](https://example.com/path?a=1&b=2 \"Title \\\"q\\\" <x>\")",
			want: "<p><a href=\"https://example.com/path?a=1&amp;b=2\" title=\"Title &quot;q&quot; &lt;x&gt;\">text</a></p>\n",
		},
		{
			name: "reference links",
			src: "[text][ref] and [ref]\n" +
				"\n" +
				"[ref]: https://example.com/ref 'Ref title'",
			want: "<p><a href=\"https://example.com/ref\" title=\"Ref title\">text</a> and <a href=\"https://example.com/ref\" title=\"Ref title\">ref</a></p>\n",
		},
		{
			name: "relative and fragment links",
			src:  "[rel](docs/README.md) [frag](#section) [root](/owner/repo) [query](?tab=1)",
			want: "<p><a href=\"docs/README.md\">rel</a> <a href=\"#section\">frag</a> <a href=\"/owner/repo\">root</a> <a href=\"?tab=1\">query</a></p>\n",
		},
		{
			name: "link destinations needing escapes",
			src:  "[sp](<a b.md>) [uni](https://example.com/ü) [q](https://example.com/\"x\") [pct](https://example.com/a%20b) [amp](https://example.com/?a=1&amp;b=2)",
			want: "<p><a href=\"a%20b.md\">sp</a> <a href=\"https://example.com/%C3%BC\">uni</a> <a href=\"https://example.com/%22x%22\">q</a> <a href=\"https://example.com/a%20b\">pct</a> <a href=\"https://example.com/?a=1&amp;b=2\">amp</a></p>\n",
		},
		{
			name: "link text with markup",
			src:  "[**bold** `code` <b>raw</b>](https://example.com)",
			want: "<p><a href=\"https://example.com\"><strong>bold</strong> <code>code</code> <!-- raw HTML omitted -->raw<!-- raw HTML omitted --></a></p>\n",
		},
		{
			name: "javascript links",
			src:  "[a](javascript:alert(1)) [b](JavaScript:alert(1)) [c](< javascript:alert(1)>) [d](vbscript:msgbox(1)) [e](data:text/html;base64,PHNjcmlwdD4=)",
			want: "<p><a href=\"#\">a</a> <a href=\"#\">b</a> <a href=\"#\">c</a> <a href=\"#\">d</a> <a href=\"#\">e</a></p>\n",
		},
		{
			name: "encoded javascript links",
			src:  "[a](&#106;avascript:alert(1)) [b](javascript&#58;alert(1)) [c](javascript\\:alert(1)) [d](java&#x73;cript:alert(1)) [e](&#x6A;avascript&colon;alert(1))",
			want: "<p><a href=\"\">a</a> <a href=\"\">b</a> <a href=\"\">c</a> <a href=\"\">d</a> <a href=\"\">e</a></p>\n",
		},
		{
			name: "javascript reference link",
			src: "[x][evil]\n" +
				"\n" +
				"[evil]: javascript:alert(1)",
			want: "<p><a href=\"#\">x</a></p>\n",
		},
		{
			name: "file and data image links",
			src:  "[f](file:///etc/passwd) [png](data:image/png;base64,iVBORw0KGgo=)",
			want: "<p><a href=\"\">f</a> <a href=\"#\">png</a></p>\n",
		},
		{
			name: "images",
			src:  "![alt *text*](https://example.com/a.png \"Title\") ![](/rel.png)",
			want: "<p><img src=\"https://example.com/a.png\" alt=\"alt text\" title=\"Title\"> <img src=\"/rel.png\" alt=\"\"></p>\n",
		},
		{
			name: "image alt escaping",
			src:  "![a \"q\" <b>raw</b> & more](x.png)",
			want: "<p><img src=\"x.png\" alt=\"a &quot;q&quot; raw &amp; more\"></p>\n",
		},
		{
			name: "dangerous images",
			src:  "![a](javascript:alert(1)) ![b](data:image/svg+xml;base64,PHN2Zz4=) ![c](data:image/png;base64,iVBORw0KGgo=) ![d](&#106;avascript:alert(1)) ![e](vbscript:x)",
			want: "<p><img src=\"\" alt=\"a\"> <img src=\"\" alt=\"b\"> <img src=\"\" alt=\"c\"> <img src=\"\" alt=\"d\"> <img src=\"\" alt=\"e\"></p>\n",
		},
		{
			name: "autolinks",
			src:  "<https://example.com/a?b=1&c=2> <mailto:me@example.com> <me@example.com>",
			want: "<p><a href=\"https://example.com/a?b=1&amp;c=2\">https://example.com/a?b=1&amp;c=2</a> <a href=\"mailto:me@example.com\">mailto:me@example.com</a> <a href=\"mailto:me@example.com\">me@example.com</a></p>\n",
		},
		{
			name: "dangerous autolinks",
			src:  "<javascript:alert(1)> <JAVASCRIPT:alert(1)> <vbscript:x> <data:text/html,x> <file:///etc/passwd>",
			want: "<p><a href=\"\">javascript:alert(1)</a> <a href=\"\">JAVASCRIPT:alert(1)</a> <a href=\"\">vbscript:x</a> <a href=\"\">data:text/html,x</a> <a href=\"\">file:///etc/passwd</a></p>\n",
		},
		{
			name: "linkify",
			src:  "Visit https://example.com/path?q=1, www.example.com or me@example.com. Also ftp://example.com/f, but not javascript:alert(1).",
			want: "<p>Visit <a href=\"https://example.com/path?q=1\">https://example.com/path?q=1</a>, <a href=\"http://www.example.com\">www.example.com</a> or <a href=\"mailto:me@example.com\">me@example.com</a>. Also <a href=\"ftp://example.com/f\">ftp://example.com/f</a>, but not javascript:alert(1).</p>\n",
		},
		{
			name: "linkify trailing punctuation",
			src:  "See (https://example.com/a), https://example.com/b_(c). and www.example.com/d?",
			want: "<p>See (<a href=\"https://example.com/a\">https://example.com/a</a>), <a href=\"https://example.com/b_(c)\">https://example.com/b_(c)</a>. and <a href=\"http://www.example.com/d\">www.example.com/d</a>?</p>\n",
		},
	})
}

func TestRender_RawHTML(t *testing.T) {
	runRenderCases(t, []renderCase{
		{
			name: "inline raw html",
			src:  "a <script>alert(1)</script> b <img src=x onerror=alert(1)> c <a href=\"javascript:alert(1)\">d</a>",
			want: "<p>a <!-- raw HTML omitted -->alert(1)<!-- raw HTML omitted --> b <!-- raw HTML omitted --> c <!-- raw HTML omitted -->d<!-- raw HTML omitted --></p>\n",
		},
		{
			name: "script block",
			src: "<script>\n" +
				"alert(1)\n" +
				"</script>\n" +
				"\n" +
				"after",
			want: "<!-- raw HTML omitted -->\n" +
				"<!-- raw HTML omitted -->\n" +
				"<p>after</p>\n",
		},
		{
			name: "img block",
			src:  "<img src=x onerror=alert(1)>",
			want: "<!-- raw HTML omitted -->\n",
		},
		{
			name: "div block",
			src: "<div onclick=\"alert(1)\">\n" +
				"*md*\n" +
				"</div>",
			want: "<!-- raw HTML omitted -->\n",
		},
		{
			name: "comment, style and iframe blocks",
			src: "<!-- hidden -->\n" +
				"\n" +
				"<style>body{display:none}</style>\n" +
				"\n" +
				"<iframe src=\"https://evil.example\"></iframe>",
			want: "<!-- raw HTML omitted -->\n" +
				"<!-- raw HTML omitted -->\n" +
				"<!-- raw HTML omitted -->\n",
		},
		{
			name: "raw html inside emphasis",
			src:  "**<em>x</em>**",
			want: "<p><strong><!-- raw HTML omitted -->x<!-- raw HTML omitted --></strong></p>\n",
		},
	})
}

func TestRender_Code(t *testing.T) {
	runRenderCases(t, []renderCase{
		{
			name: "inline code",
			src:  "use `a < b && c` and ``x ` y``",
			want: "<p>use <code>a &lt; b &amp;&amp; c</code> and <code>x ` y</code></p>\n",
		},
		{
			name: "code span across lines",
			src: "`one\n" +
				"two` and `` `tick` ``",
			want: "<p><code>one two</code> and <code>`tick`</code></p>\n",
		},
		{
			name: "fenced code with language",
			src: "```go\n" +
				"fmt.Println(\"<hi>\", 'x', a&b)\n" +
				"```",
			want: "<pre><code class=\"language-go\">fmt.Println(&#34;&lt;hi&gt;&#34;, &#39;x&#39;, a&amp;b)\n" +
				"</code></pre>\n",
		},
		{
			name: "fenced code without language",
			src: "```\n" +
				"plain <b>text</b>\n" +
				"```",
			want: "<pre><code>plain &lt;b&gt;text&lt;/b&gt;\n" +
				"</code></pre>\n",
		},
		{
			name: "tilde fence with extra info",
			src: "~~~python linenos=1\n" +
				"print('hi')\n" +
				"~~~",
			want: "<pre><code class=\"language-python\">print(&#39;hi&#39;)\n" +
				"</code></pre>\n",
		},
		{
			name: "info string with html",
			src: "```\"><script>alert(1)</script>\n" +
				"code\n" +
				"```",
			want: "<pre><code class=\"language-&#34;&gt;&lt;script&gt;alert(1)&lt;/script&gt;\">code\n" +
				"</code></pre>\n",
		},
		{
			name: "info string with entity",
			src: "```c&#43;&#43;\n" +
				"int x;\n" +
				"```",
			want: "<pre><code class=\"language-c&amp;#43;&amp;#43;\">int x;\n" +
				"</code></pre>\n",
		},
		{
			name: "indented code block",
			src: "    indented <code> & 'q'\n" +
				"    line 2",
			want: "<pre><code>indented &lt;code&gt; &amp; 'q'\n" +
				"line 2\n" +
				"</code></pre>\n",
		},
		{
			name: "unclosed fence",
			src: "```js\n" +
				"let x = 1;",
			want: "<pre><code class=\"language-js\">let x = 1;\n" +
				"</code></pre>\n",
		},
		{
			name: "fenced code in list",
			src: "- item\n" +
				"\n" +
				"  ```sh\n" +
				"  echo \"hi\"\n" +
				"  ```",
			want: "<ul>\n" +
				"<li>\n" +
				"<p>item</p>\n" +
				"<pre><code class=\"language-sh\">echo &#34;hi&#34;\n" +
				"</code></pre>\n" +
				"</li>\n" +
				"</ul>\n",
		},
		{
			name: "empty fenced block",
			src: "```go\n" +
				"```",
			want: "<pre><code class=\"language-go\"></code></pre>\n",
		},
		{
			name: "mermaid fence",
			src: "```mermaid\n" +
				"graph TD\n" +
				"  A[\"Start\"] --> B{Is it?}\n" +
				"  B -->|Yes| C['ok']\n" +
				"```",
			want: "<pre class=\"mermaid\">graph TD\n" +
				"  A[&#34;Start&#34;] --&gt; B{Is it?}\n" +
				"  B --&gt;|Yes| C[&#39;ok&#39;]\n" +
				"</pre>\n",
		},
		{
			name: "mermaid with html payload",
			src: "```mermaid\n" +
				"graph LR\n" +
				"  A[\"<img src=x onerror=alert(1)>\"] --> B[</pre><script>alert(1)</script>]\n" +
				"```",
			want: "<pre class=\"mermaid\">graph LR\n" +
				"  A[&#34;&lt;img src=x onerror=alert(1)&gt;&#34;] --&gt; B[&lt;/pre&gt;&lt;script&gt;alert(1)&lt;/script&gt;]\n" +
				"</pre>\n",
		},
		{
			name: "mermaid info string with attributes",
			src: "```mermaid {theme: dark}\n" +
				"graph TD\n" +
				"```",
			want: "<pre class=\"mermaid\">graph TD\n" +
				"</pre>\n",
		},
		{
			name: "tilde mermaid fence",
			src: "~~~mermaid\n" +
				"graph TD\n" +
				"~~~",
			want: "<pre class=\"mermaid\">graph TD\n" +
				"</pre>\n",
		},
		{
			name: "mermaid is case sensitive",
			src: "```Mermaid\n" +
				"graph TD\n" +
				"```",
			want: "<pre><code class=\"language-Mermaid\">graph TD\n" +
				"</code></pre>\n",
		},
		{
			name: "indented mermaid fence is plain code",
			src: "    ```mermaid\n" +
				"    graph TD\n" +
				"    ```",
			want: "<pre><code>```mermaid\n" +
				"graph TD\n" +
				"```\n" +
				"</code></pre>\n",
		},
		{
			name: "mermaid in blockquote",
			src: "> ```mermaid\n" +
				"> graph TD\n" +
				"> ```",
			want: "<blockquote>\n" +
				"<pre class=\"mermaid\">graph TD\n" +
				"</pre>\n" +
				"</blockquote>\n",
		},
		{
			name: "empty mermaid fence",
			src: "```mermaid\n" +
				"```",
			want: "<pre class=\"mermaid\"></pre>\n",
		},
	})
}
