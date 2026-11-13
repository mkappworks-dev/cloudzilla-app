package secretscan

import (
	"math"
	"regexp"
	"strings"
)

// Confidence says how sure a pattern is; the caller decides what blocks.
type Confidence int

const (
	Low Confidence = iota + 1
	High
)

func (c Confidence) String() string {
	if c == High {
		return "high"
	}
	return "low"
}

// Pattern is one entry of the static set. Group is the capture group holding
// the secret (0 for the whole match).
type Pattern struct {
	Type       string
	Confidence Confidence
	Re         *regexp.Regexp
	Group      int
	accept     func(value string) bool
	// spanEnd extends the value from the match through the first match of spanEnd, so a multi-line secret is
	// identified by its material and not by its header.
	spanEnd *regexp.Regexp
}

const minGenericEntropy = 3.5

var placeholderWords = []string{"your", "example", "placeholder", "changeme", "xxxx", "<", "todo"}

// Patterns is ordered high confidence first: Scan drops a lower-confidence
// finding that repeats a value already found on the same line.
var Patterns = []Pattern{
	{
		Type:       "aws_access_key_id",
		Confidence: High,
		Re:         regexp.MustCompile(`\b(?:AKIA|ASIA)[A-Z2-7]{16}\b`),
	},
	{
		Type:       "private_key",
		Confidence: High,
		Re:         regexp.MustCompile(`-----BEGIN (?:[A-Z]+ )*PRIVATE KEY(?: BLOCK)?-----`),
		spanEnd:    regexp.MustCompile(`-----END (?:[A-Z]+ )*PRIVATE KEY(?: BLOCK)?-----`),
	},
	{
		Type:       "cloudzilla_token",
		Confidence: High,
		Re:         regexp.MustCompile(`\bczp_[0-9a-f]{64}\b`),
	},
	{
		Type:       "jwt_secret",
		Confidence: High,
		Re:         regexp.MustCompile(`(?i)\bjwt[_-]?secret\b["']?\s*[:=]\s*["']?([^\s"'$\{][^\s"']{15,})`),
		Group:      1,
	},
	{
		Type:       "generic_api_key",
		Confidence: Low,
		Re:         regexp.MustCompile(`(?i)(?:api[_-]?key|secret|token)["']?\s*[:=]\s*["']([A-Za-z0-9+/_=.-]{20,})["']`),
		Group:      1,
		accept:     looksRandom,
	},
}

func looksRandom(v string) bool {
	lower := strings.ToLower(v)
	for _, w := range placeholderWords {
		if strings.Contains(lower, w) {
			return false
		}
	}
	return entropy(v) >= minGenericEntropy
}

// entropy is Shannon entropy in bits per character.
func entropy(s string) float64 {
	counts := map[rune]int{}
	n := 0
	for _, r := range s {
		counts[r]++
		n++
	}
	var h float64
	for _, c := range counts {
		p := float64(c) / float64(n)
		h -= p * math.Log2(p)
	}
	return h
}
