package backup

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestManifest_RoundTripsThroughItsFixedSlot(t *testing.T) {
	want := Manifest{
		FormatVersion: FormatVersion, CloudzillaVersion: "v1.2.3", CreatedAt: time.Date(2026, 10, 8, 3, 0, 0, 0, time.UTC),
		Migration: "104_x", PgDumpVersion: "18.6",
		Sections:      map[string]Section{SectionGitRepos: {Files: 3, Bytes: 42}},
		ObjectStorage: &ObjectStorage{Backend: "s3", Bucket: "cz"},
	}
	slot, err := want.slot()
	if err != nil {
		t.Fatal(err)
	}
	if len(slot) != manifestSlot {
		t.Fatalf("slot is %d bytes, want %d", len(slot), manifestSlot)
	}
	got, err := decodeManifest(bytes.NewReader(slot))
	if err != nil {
		t.Fatal(err)
	}
	if got.Migration != want.Migration || got.Sections[SectionGitRepos] != want.Sections[SectionGitRepos] || got.ObjectStorage.Bucket != "cz" || !got.CreatedAt.Equal(want.CreatedAt) {
		t.Errorf("round trip changed the manifest: %+v", got)
	}
}

func TestDecodeManifest_RejectsUnknownFormat(t *testing.T) {
	_, err := decodeManifest(strings.NewReader(`{"format_version": 99}`))
	if err == nil || !strings.Contains(err.Error(), "format version 99") {
		t.Fatalf("err = %v, want a format version refusal", err)
	}
}

func TestCheckEntry(t *testing.T) {
	reg, dir := byte(tar.TypeReg), byte(tar.TypeDir)
	for _, c := range []struct {
		name    string
		typ     byte
		wantSec string
		wantRel string
		bad     string
	}{
		{name: "cloudzilla-backup.json", typ: reg, wantSec: SectionManifest},
		{name: "database.pgdump", typ: reg, wantSec: SectionDatabase},
		{name: "ssh_host_key", typ: reg, wantSec: SectionHostKey},
		{name: "git-repos", typ: dir, wantSec: SectionGitRepos, wantRel: ""},
		{name: "git-repos/", typ: dir, wantSec: SectionGitRepos, wantRel: ""},
		{name: "git-repos/alice/app.git/HEAD", typ: reg, wantSec: SectionGitRepos, wantRel: "alice/app.git/HEAD"},
		{name: "git-repos/alice/app.git/", typ: dir, wantSec: SectionGitRepos, wantRel: "alice/app.git"},
		{name: "storage", typ: dir, wantSec: SectionStorage, wantRel: ""},
		{name: "storage/avatars/user/1/ab.png", typ: reg, wantSec: SectionStorage, wantRel: "avatars/user/1/ab.png"},
		{name: "storage/../x", typ: reg, bad: "unsafe"},
		{name: "storage", typ: reg, bad: "type"},
		{name: "git-repos/../etc/passwd", typ: reg, bad: "unsafe"},
		{name: "git-repos/a/../../b", typ: reg, bad: "unsafe"},
		{name: "/etc/passwd", typ: reg, bad: "unsafe"},
		{name: "git-repos//a", typ: reg, bad: "unsafe"},
		{name: "git-repos/./a", typ: reg, bad: "unsafe"},
		{name: "git-repos/a\x00b", typ: reg, bad: "unsafe"},
		{name: "git-repos/link", typ: tar.TypeSymlink, bad: "type"},
		{name: "git-repos/hard", typ: tar.TypeLink, bad: "type"},
		{name: "git-repos/dev", typ: tar.TypeChar, bad: "type"},
		{name: "other/file", typ: reg, bad: "unexpected"},
		{name: "database.pgdump/x", typ: reg, bad: "unexpected"},
		{name: "ssh_host_key", typ: dir, bad: "type"},
	} {
		sec, rel, err := checkEntry(c.name, c.typ)
		if c.bad != "" {
			if err == nil || !strings.Contains(err.Error(), c.bad) {
				t.Errorf("checkEntry(%q, %q) = %v, want an error containing %q", c.name, c.typ, err, c.bad)
			}
			continue
		}
		if err != nil || sec != c.wantSec || rel != c.wantRel {
			t.Errorf("checkEntry(%q) = %q, %q, %v; want %q, %q", c.name, sec, rel, err, c.wantSec, c.wantRel)
		}
	}
}
