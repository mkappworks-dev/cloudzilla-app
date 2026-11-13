package pages_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view/pages"
)

func TestSecurityAlerts_HistoryScanBanners(t *testing.T) {
	cases := []struct {
		name               string
		scanning, partial  bool
		wantScan, wantPart bool
	}{
		{"scan finished", false, false, false, false},
		{"scan pending or running", true, false, true, false},
		{"scan stopped at its budget", false, true, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var sb strings.Builder
			data := view.SecretAlertsData{Owner: "o", RepoName: "r", State: "open", HistoryScanning: c.scanning, HistoryIncomplete: c.partial}
			if err := pages.SecurityAlerts(data).Render(context.Background(), &sb); err != nil {
				t.Fatal(err)
			}
			out := sb.String()
			if got := strings.Contains(out, "Scanning existing history"); got != c.wantScan {
				t.Errorf("scanning banner = %v, want %v", got, c.wantScan)
			}
			if got := strings.Contains(out, "History scan incomplete"); got != c.wantPart {
				t.Errorf("incomplete banner = %v, want %v", got, c.wantPart)
			}
		})
	}
}
