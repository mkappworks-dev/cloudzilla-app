// Package logmask hides secret values in job logs. The runner applies it before upload and the server again as a
// backstop, so a runner that skips it still cannot store a secret.
package logmask

import (
	"encoding/base64"
	"net/url"
	"slices"
	"strings"
)

const Redacted = "***"

// minLineLen is the shortest line of a multi-line secret that is masked on its own; shorter ones ("EOF", "}") would
// redact ordinary log text.
const minLineLen = 4

// Masker replaces each secret, and its base64 and URL-encoded forms, with Redacted. The zero value and nil mask nothing.
type Masker struct {
	r       *strings.Replacer
	secrets []string
}

func addForms(add func(string), s string) {
	add(s)
	add(base64.StdEncoding.EncodeToString([]byte(s)))
	add(base64.URLEncoding.EncodeToString([]byte(s)))
	add(base64.RawStdEncoding.EncodeToString([]byte(s)))
	add(base64.RawURLEncoding.EncodeToString([]byte(s)))
	add(url.QueryEscape(s))
	add(url.PathEscape(s))
}

func New(secrets []string) *Masker {
	seen := map[string]bool{}
	var forms []string
	add := func(s string) {
		if s != "" && !seen[s] {
			seen[s] = true
			forms = append(forms, s)
		}
	}
	for _, s := range secrets {
		if s == "" {
			continue
		}
		if strings.ContainsAny(s, "\r\n") {
			// Log chunks end at line breaks, so a multi-line secret can straddle two chunks: mask each line alone.
			for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' }) {
				if len(line) >= minLineLen {
					addForms(add, line)
				}
			}
		}
		addForms(add, s)
	}
	if len(forms) == 0 {
		return &Masker{}
	}
	// strings.Replacer tries pairs in argument order at each position, so the longest comes first.
	slices.SortFunc(forms, func(a, b string) int { return len(b) - len(a) })
	pairs := make([]string, 0, 2*len(forms))
	for _, f := range forms {
		pairs = append(pairs, f, Redacted)
	}
	return &Masker{r: strings.NewReplacer(pairs...), secrets: slices.Clone(secrets)}
}

// Combine masks everything any of ms masks; nil entries add nothing.
func Combine(ms ...*Masker) *Masker {
	var all []string
	for _, m := range ms {
		if m != nil {
			all = append(all, m.secrets...)
		}
	}
	return New(all)
}

func (m *Masker) Mask(s string) string {
	if m == nil || m.r == nil {
		return s
	}
	return m.r.Replace(s)
}

func (m *Masker) MaskBytes(b []byte) []byte {
	if m == nil || m.r == nil {
		return b
	}
	return []byte(m.r.Replace(string(b)))
}
