package model

import (
	"reflect"
	"testing"
)

func TestSnippetParts(t *testing.T) {
	r := CodeSearchResult{Snippet: "x := **p + " + SnippetMarkStart + "Router" + SnippetMarkEnd + "(a)"}
	want := []SnippetPart{{Text: "x := **p + "}, {Text: "Router", Match: true}, {Text: "(a)"}}
	if got := r.SnippetParts(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got := (CodeSearchResult{}).SnippetParts(); got != nil {
		t.Fatalf("empty snippet: got %+v", got)
	}
}
