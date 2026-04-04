package service

import (
	"testing"
	"time"
)

func TestWebhookBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		wantMin time.Duration
		wantMax time.Duration
	}{
		{attempt: 1, wantMin: 1 * time.Minute, wantMax: 1*time.Minute + time.Second},
		{attempt: 2, wantMin: 5 * time.Minute, wantMax: 5*time.Minute + time.Second},
		{attempt: 3, wantMin: 30 * time.Minute, wantMax: 30*time.Minute + time.Second},
		{attempt: 4, wantMin: 2 * time.Hour, wantMax: 2*time.Hour + time.Second},
		{attempt: 5, wantMin: 8 * time.Hour, wantMax: 8*time.Hour + time.Second},
		// Any attempt beyond 5 should clamp to 8 hours
		{attempt: 10, wantMin: 8 * time.Hour, wantMax: 8*time.Hour + time.Second},
	}
	for _, tt := range tests {
		d := webhookBackoff(tt.attempt)
		if d < tt.wantMin || d > tt.wantMax {
			t.Errorf("webhookBackoff(%d) = %v, want [%v, %v]", tt.attempt, d, tt.wantMin, tt.wantMax)
		}
	}
}
