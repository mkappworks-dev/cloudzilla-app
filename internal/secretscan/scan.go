// Package secretscan recognises secrets in file content. It is pure: no
// database, no HTTP, and it never decides what blocks.
package secretscan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

const (
	binarySniffBytes = 8 << 10
	maxFindings      = 50
	maskKeep         = 4
	// A value this short would be mostly revealed by first-and-last-four.
	minMaskable = 12
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
	for i, line := range bytes.Split(content, []byte{'\n'}) {
		start := len(out)
		for _, p := range Patterns {
			for _, m := range p.Re.FindAllSubmatch(line, -1) {
				value := string(m[p.Group])
				if p.accept != nil && !p.accept(value) {
					continue
				}
				if seenOnLine(out[start:], value) {
					continue
				}
				out = append(out, Finding{Type: p.Type, Line: i + 1, Value: value, Confidence: p.Confidence})
				if len(out) == maxFindings {
					return out
				}
			}
		}
	}
	return out
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
