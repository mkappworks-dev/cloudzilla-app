package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newMirrorImportEnv(t *testing.T, mirror config.MirrorConfig, key string) (*service.Services, *store.MirrorStore, int64, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	cfg := &config.Config{
		Git:      config.GitConfig{ReposRoot: t.TempDir()},
		Import:   config.ImportConfig{AllowLocalNetworks: true, Timeout: time.Minute},
		Security: config.SecurityConfig{SecretKey: key},
		Mirror:   mirror,
	}
	uid, uname := seedImportUser(t, db)
	return service.New(store.New(db), cfg), store.NewMirrorStore(db), uid, uname
}

var mirrorsOn = config.MirrorConfig{Enabled: true, AllowLocalNetworks: true, MinInterval: 10 * time.Minute,
	DefaultInterval: 8 * time.Hour, MaxConcurrent: 1, Timeout: time.Minute}

func TestImport_Mirror_CreatesThePullMirror(t *testing.T) {
	svc, mirrors, uid, uname := newMirrorImportEnv(t, mirrorsOn, strings.Repeat("k", 32))
	ctx := context.Background()
	src := testutil.SeedSourceRepo(t)
	url := testutil.ServeGitHTTP(t, src.Dir, "alice", "s3cret")

	start := time.Now()
	job, err := svc.Import.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: url, AuthUsername: "alice", AuthToken: "s3cret", Name: "mirrored",
		Mirror: true, MirrorInterval: 2 * time.Hour,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if done := waitImport(t, svc.Import, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("status %s: %s", done.Status, done.Error)
	}

	repo, err := svc.Repo.Get(ctx, uname, "mirrored")
	if err != nil || !repo.IsMirror {
		t.Fatalf("repo = %+v, %v; want a mirror", repo, err)
	}
	m, err := mirrors.Get(ctx, repo.ID)
	if err != nil {
		t.Fatalf("mirror row: %v", err)
	}
	if m.RemoteURL != url || m.AuthUsername != "alice" || m.Interval != 2*time.Hour || m.CreatedBy != uid {
		t.Errorf("mirror = %+v", m)
	}
	if due := m.NextSyncAt.Sub(start); due < 2*time.Hour || due > 2*time.Hour+time.Minute {
		t.Errorf("next sync %v after the import started, want one interval", due)
	}
	if len(m.AuthTokenEnc) == 0 || strings.Contains(string(m.AuthTokenEnc), "s3cret") {
		t.Errorf("stored token = %q, want it sealed", m.AuthTokenEnc)
	}
	// The sealed token opens: the first scheduled sync can authenticate.
	if err := svc.Mirror.Sync(ctx, m); err != nil {
		t.Errorf("Sync with the stored token: %v", err)
	}
}

func TestImport_Mirror_DefaultInterval(t *testing.T) {
	svc, mirrors, uid, uname := newMirrorImportEnv(t, mirrorsOn, "")
	ctx := context.Background()
	job, err := svc.Import.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", ""), Name: "public-mirror", Mirror: true,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if done := waitImport(t, svc.Import, uid, job.ID); done.Status != service.ImportDone {
		t.Fatalf("status %s: %s", done.Status, done.Error)
	}
	repo, _ := svc.Repo.Get(ctx, uname, "public-mirror")
	m, err := mirrors.Get(ctx, repo.ID)
	if err != nil || m.Interval != 8*time.Hour || m.AuthTokenEnc != nil {
		t.Errorf("mirror = %+v, %v; want the 8h default and no token", m, err)
	}
}

func TestImport_PlainImport_IsNoMirror(t *testing.T) {
	svc, mirrors, uid, uname := newMirrorImportEnv(t, mirrorsOn, "")
	ctx := context.Background()
	job, err := svc.Import.Start(ctx, uid, uname, service.ImportRequest{
		CloneURL: testutil.ServeGitHTTP(t, testutil.SeedSourceRepo(t).Dir, "", ""), Name: "copy",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	waitImport(t, svc.Import, uid, job.ID)
	repo, err := svc.Repo.Get(ctx, uname, "copy")
	if err != nil || repo.IsMirror {
		t.Fatalf("repo = %+v, %v; want a plain repo", repo, err)
	}
	if _, err := mirrors.Get(ctx, repo.ID); err == nil {
		t.Error("a plain import got a mirror row")
	}
}

func TestImport_Mirror_Refusals(t *testing.T) {
	off := mirrorsOn
	off.Enabled = false
	for _, tc := range []struct {
		name    string
		mirror  config.MirrorConfig
		key     string
		req     service.ImportRequest
		wantErr error
		wantMsg string
	}{
		{"mirrors off", off, "", service.ImportRequest{Mirror: true}, service.ErrMirrorsDisabled, ""},
		{"too short", mirrorsOn, "", service.ImportRequest{Mirror: true, MirrorInterval: 5 * time.Minute}, service.ErrMirrorInterval, "10 minutes"},
		{"too long", mirrorsOn, "", service.ImportRequest{Mirror: true, MirrorInterval: 31 * 24 * time.Hour}, service.ErrMirrorInterval, "30 days"},
		{"token without a key", mirrorsOn, "", service.ImportRequest{Mirror: true, AuthUsername: "a", AuthToken: "t"}, service.ErrMirrorNoSecretKey, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, uid, uname := newMirrorImportEnv(t, tc.mirror, tc.key)
			req := tc.req
			req.CloneURL, req.Name = "https://example.com/up.git", "refused"
			_, err := svc.Import.Start(context.Background(), uid, uname, req)
			if !errors.Is(err, tc.wantErr) || !strings.Contains(err.Error(), tc.wantMsg) {
				t.Errorf("Start err = %v, want %v mentioning %q", err, tc.wantErr, tc.wantMsg)
			}
		})
	}
}

func TestMirrorService_IntervalChoices(t *testing.T) {
	cfg := mirrorsOn
	cfg.MinInterval, cfg.DefaultInterval = time.Hour, 12*time.Hour
	svc := service.New(store.New(nil), &config.Config{Mirror: cfg})

	var got []string
	var selected string
	for _, c := range svc.Mirror.IntervalChoices() {
		got = append(got, c.Label)
		if c.Default {
			selected = c.Label
		}
	}
	if want := "1 hour,8 hours,12 hours,1 day,1 week"; strings.Join(got, ",") != want {
		t.Errorf("choices = %v, want %s", got, want)
	}
	if selected != "12 hours" {
		t.Errorf("default = %q, want 12 hours", selected)
	}
}
