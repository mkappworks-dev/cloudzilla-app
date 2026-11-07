package handler

import "testing"

func TestMergeCommitMessage(t *testing.T) {
	if m, err := mergeCommitMessage(" ", ""); m != "" || err != nil {
		t.Errorf("blank = (%q, %v), want the default", m, err)
	}
	if _, err := mergeCommitMessage("", "body"); err == nil {
		t.Error("a body with no title must be refused")
	}
	if m, _ := mergeCommitMessage("T", "B"); m != "T\n\nB" {
		t.Errorf("joined = %q", m)
	}
}
