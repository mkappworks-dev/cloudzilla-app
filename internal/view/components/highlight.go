package components

import (
	"strings"
	"unicode"
)

type textSegment struct {
	Text  string
	Match bool
}

// queryTokens splits like the store's prefixTSQuery, so a word is marked exactly when the search
// would have matched it by prefix.
func queryTokens(query string) []string {
	return strings.FieldsFunc(strings.ToLower(query), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// highlightSegments cuts text into runs, marking each word that starts with a query token.
// A match in the middle of a word is not a match: the search is by prefix.
func highlightSegments(text, query string) []textSegment {
	tokens := queryTokens(query)
	if len(tokens) == 0 || text == "" {
		return []textSegment{{Text: text}}
	}
	isWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

	var segs []textSegment
	runes := []rune(text)
	for start := 0; start < len(runes); {
		end := start
		word := isWord(runes[start])
		for end < len(runes) && isWord(runes[end]) == word {
			end++
		}
		run := string(runes[start:end])
		segs = append(segs, textSegment{Text: run, Match: word && hasAnyPrefix(strings.ToLower(run), tokens)})
		start = end
	}
	return segs
}

func hasAnyPrefix(word string, tokens []string) bool {
	for _, t := range tokens {
		if strings.HasPrefix(word, t) {
			return true
		}
	}
	return false
}
