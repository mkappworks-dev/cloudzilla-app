// Package secretscan recognises secrets in file content. It is pure: no
// database, no HTTP, and it never decides what blocks.
package secretscan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
)

const (
	binarySniffBytes = 8 << 10
	maxFindings      = 50
	maskKeep         = 4
	// A value this short would be mostly revealed by first-and-last-four.
	minMaskable = 12
	// Covers any real key; a longer value would hash a whole file for one secret.
	maxSpanBytes = 16 << 10
)

// Finding is one secret in a blob. Value stays in memory: it is never
// printed, so a Finding is safe to hand to a logger.
type Finding struct {
	Type       string
	Line       int
	Value      string
	Confidence Confidence
}

func (f Finding) String() string { return f.Type + "@line " + strconv.Itoa(f.Line) }

func (f Finding) GoString() string { return "secretscan.Finding{" + f.String() + "}" }

func (f Finding) LogValue() slog.Value {
	return slog.GroupValue(slog.String("type", f.Type), slog.Int("line", f.Line))
}

// Scan returns the findings in content, at most 50. Binary content (a NUL in
// the first 8 KiB) and content longer than maxBytes yield nothing. path is
// reserved for per-path rules.
func Scan(path string, content []byte, maxBytes int) []Finding {
	if len(content) > maxBytes || bytes.IndexByte(content[:min(len(content), binarySniffBytes)], 0) >= 0 {
		return nil
	}
	var out []Finding
	for off, i := 0, 1; ; i++ {
		end := len(content)
		if nl := bytes.IndexByte(content[off:], '\n'); nl >= 0 {
			end = off + nl
		}
		line := content[off:end]
		start := len(out)
		for _, p := range Patterns {
			for _, m := range p.Re.FindAllSubmatchIndex(line, -1) {
				value := string(line[m[2*p.Group]:m[2*p.Group+1]])
				if p.spanEnd != nil {
					value = spanValue(content[off+m[0]:], p.spanEnd)
				}
				if p.accept != nil && !p.accept(value) {
					continue
				}
				if seenOnLine(out[start:], value) {
					continue
				}
				out = append(out, Finding{Type: p.Type, Line: i, Value: value, Confidence: p.Confidence})
				if len(out) == maxFindings {
					return out
				}
			}
		}
		if end == len(content) {
			return out
		}
		off = end + 1
	}
}

// spanValue is rest up to and including the first match of endRe, or up to maxSpanBytes of it when the end marker is
// missing or too far away.
func spanValue(rest []byte, endRe *regexp.Regexp) string {
	rest = rest[:min(len(rest), maxSpanBytes)]
	if loc := endRe.FindIndex(rest); loc != nil {
		rest = rest[:loc[1]]
	}
	return string(rest)
}

func seenOnLine(found []Finding, value string) bool {
	for _, f := range found {
		if f.Value == value {
			return true
		}
	}
	return false
}

// Mask returns a hint of at most eight characters: the first and last four of
// the value, or the key kind of a private key header.
func Mask(value string) string {
	if rest, ok := strings.CutPrefix(value, "-----BEGIN "); ok {
		kind, _, _ := strings.Cut(rest, "PRIVATE KEY")
		kind = strings.TrimSpace(kind)
		if kind == "" {
			kind = "PRIVATE"
		}
		return truncateRunes(kind, 2*maskKeep)
	}
	r := []rune(value)
	if len(r) <= minMaskable {
		return "****"
	}
	return string(r[:maskKeep]) + string(r[len(r)-maskKeep:])
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// Hash is the per-repository dedupe key for a secret: sha256(repoID ":" value).
func Hash(repoID int64, value string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s", repoID, value)))
	return hex.EncodeToString(sum[:])
}
