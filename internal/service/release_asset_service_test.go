package service_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

func TestCleanReleaseAssetName(t *testing.T) {
	long := strings.Repeat("a", 150)
	for _, tc := range []struct {
		in, want string
		err      error
	}{
		{"app-1.2.3.tar.gz", "app-1.2.3.tar.gz", nil},
		{"my app (final).zip", "my-app--final-.zip", nil},
		{"../../etc/passwd", "passwd", nil},
		{`C:\Users\me\app.exe`, "app.exe", nil},
		{"naïve.txt", "na-ve.txt", nil},
		{"<script>.html", "-script-.html", nil},
		{long + ".tar.gz", long[:100-len(".gz")] + ".gz", nil},
		{"", "", service.ErrReleaseAssetName},
		{"..", "", service.ErrReleaseAssetName},
		{"...", "", service.ErrReleaseAssetName},
		{"---", "", service.ErrReleaseAssetName},
	} {
		got, err := service.CleanReleaseAssetName(tc.in)
		if got != tc.want || !errors.Is(err, tc.err) {
			t.Errorf("CleanReleaseAssetName(%q) = %q, %v; want %q, %v", tc.in, got, err, tc.want, tc.err)
		}
		if len(got) > 100 {
			t.Errorf("%q: %d chars", tc.in, len(got))
		}
	}
}
