package service

import (
	"encoding/json"
	"testing"
)

func TestEventPayloadMarshal(t *testing.T) {
	payload := map[string]any{
		"branch": "main",
		"ref":    "refs/heads/main",
		"size":   3,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if out["branch"] != "main" {
		t.Errorf("expected branch=main, got %v", out["branch"])
	}
}

func TestFeedPageClamp(t *testing.T) {
	clamp := func(page, pageSize int) (int, int) {
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 30
		}
		return page, pageSize
	}
	p, ps := clamp(0, 0)
	if p != 1 || ps != 30 {
		t.Errorf("expected (1,30) got (%d,%d)", p, ps)
	}
	p, ps = clamp(2, 50)
	if p != 2 || ps != 50 {
		t.Errorf("expected (2,50) got (%d,%d)", p, ps)
	}
	p, ps = clamp(1, 200)
	if p != 1 || ps != 30 {
		t.Errorf("expected (1,30) got (%d,%d)", p, ps)
	}
}
