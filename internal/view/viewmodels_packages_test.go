package view_test

import (
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/view"
)

func summaries() []model.PackageSummary {
	return []model.PackageSummary{
		{Package: model.Package{Type: model.PackageTypeContainer, Name: "cloudzilla-server"}},
		{Package: model.Package{Type: model.PackageTypeGeneric, Name: "cz-cli"}},
		{Package: model.Package{Type: model.PackageTypeContainer, Name: "runner"}},
	}
}

func names(l []model.PackageSummary) []string {
	out := make([]string, len(l))
	for i, p := range l {
		out[i] = p.Name
	}
	return out
}

func TestFilterPackages(t *testing.T) {
	tests := []struct {
		name, typ, q string
		want         []string
		counts       view.PackageCounts
	}{
		{"all", "", "", []string{"cloudzilla-server", "cz-cli", "runner"}, view.PackageCounts{All: 3, Container: 2, Generic: 1}},
		{"type", "generic", "", []string{"cz-cli"}, view.PackageCounts{All: 3, Container: 2, Generic: 1}},
		{"name ignores case and counts follow it", "", "CLOUD", []string{"cloudzilla-server"}, view.PackageCounts{All: 1, Container: 1}},
		{"type and name", "container", "r", []string{"cloudzilla-server", "runner"}, view.PackageCounts{All: 2, Container: 2}},
		{"unknown type is all", "maven", "", []string{"cloudzilla-server", "cz-cli", "runner"}, view.PackageCounts{All: 3, Container: 2, Generic: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			shown, counts := view.FilterPackages(summaries(), tc.typ, tc.q)
			if got := names(shown); len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) || counts != tc.counts {
				t.Errorf("FilterPackages = %v %+v, want %v %+v", got, counts, tc.want, tc.counts)
			}
		})
	}
}

func TestPullCommand(t *testing.T) {
	tag := func(n string) model.TagRow { return model.TagRow{Name: n, Digest: "sha256:aa"} }
	untagged := []model.ManifestRow{{Digest: "sha256:bb"}}
	tests := []struct {
		name     string
		tags     []model.TagRow
		untagged []model.ManifestRow
		newest   string
		want     string
	}{
		{"newest tag", []model.TagRow{tag("v1"), tag("v2")}, nil, "v2", "docker pull git.example/o/img:v2"},
		{"falls back to the first tag", []model.TagRow{tag("v1")}, nil, "gone", "docker pull git.example/o/img:v1"},
		{"digest when only untagged", nil, untagged, "", "docker pull git.example/o/img@sha256:bb"},
		{"image only when empty", nil, nil, "", "docker pull git.example/o/img"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := view.PullCommand("git.example", "o", "img", tc.tags, tc.untagged, tc.newest); got != tc.want {
				t.Errorf("PullCommand = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRegistryHost(t *testing.T) {
	for in, want := range map[string]string{"http://localhost:8080": "localhost:8080", "https://git.example.com/": "git.example.com", "": ""} {
		if got := view.RegistryHost(in); got != want {
			t.Errorf("RegistryHost(%q) = %q, want %q", in, got, want)
		}
	}
}
