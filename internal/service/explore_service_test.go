package service

import (
	"testing"
	"time"
)

func TestPeriodToDuration(t *testing.T) {
	tests := []struct {
		period string
		want   time.Duration
	}{
		{"daily", 24 * time.Hour},
		{"weekly", 7 * 24 * time.Hour},
		{"monthly", 30 * 24 * time.Hour},
		{"", 7 * 24 * time.Hour},
		{"unknown", 7 * 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			if got := periodToDuration(tt.period); got != tt.want {
				t.Errorf("periodToDuration(%q) = %v, want %v", tt.period, got, tt.want)
			}
		})
	}
}
