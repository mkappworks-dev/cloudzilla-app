package service_test

// Integration tests for AttachmentService. They require TEST_DATABASE_DSN and skip otherwise.

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"image/color"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/attachment"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

type attachmentEnv struct {
	db      *sql.DB
	svc     *service.AttachmentService
	backend storage.Backend
	userID  int64
	repoID  int64
}

func newAttachmentEnv(t *testing.T) attachmentEnv {
	t.Helper()
	db := testutil.OpenTestDB(t)
	backend, err := storage.NewLocal(filepath.Join(t.TempDir(), "storage"))
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: t.TempDir()}}
	svcs := service.New(store.New(db), cfg).WithStorage(backend)
	suffix := testutil.UniqueSuffix(t)
	userID := testutil.SeedUser(t, db, suffix)
	repoID := testutil.SeedRepo(t, db, userID, "testuser_"+suffix, suffix)
	return attachmentEnv{db: db, svc: svcs.Attachment, backend: backend, userID: userID, repoID: repoID}
}

func (e attachmentEnv) upload(t *testing.T, c color.Color) *model.Attachment {
	t.Helper()
	a, err := e.svc.Upload(context.Background(), e.repoID, e.userID, bytes.NewReader(pngOf(t, c)))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	t.Cleanup(func() { testutil.Exec(t, e.db, `DELETE FROM attachments WHERE token = $1`, a.Token) })
	return a
}

func (e attachmentEnv) exists(t *testing.T, key string) bool {
	t.Helper()
	ok, err := e.backend.Exists(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func (e attachmentEnv) age(t *testing.T, a *model.Attachment, by time.Duration) {
	t.Helper()
	testutil.Exec(t, e.db, `UPDATE attachments SET created_at = NOW() - $2 * INTERVAL '1 second' WHERE token = $1`, a.Token, int64(by.Seconds()))
}

func (e attachmentEnv) row(t *testing.T, a *model.Attachment) bool {
	t.Helper()
	_, err := e.svc.Get(context.Background(), a.Token)
	if err != nil && !errors.Is(err, service.ErrAttachmentNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

func TestAttachmentService_UploadStoresObjectAndRow(t *testing.T) {
	e := newAttachmentEnv(t)
	a := e.upload(t, color.NRGBA{200, 0, 0, 255})

	if !strings.HasPrefix(a.StorageKey, "attachments/repo/") || !e.exists(t, a.StorageKey) {
		t.Fatalf("object not stored under attachments/repo/: %q", a.StorageKey)
	}
	if a.ContentType != "image/png" || a.RepoID != e.repoID || !strings.HasSuffix(a.URL(), ".png") || !strings.HasPrefix(a.URL(), "/attachments/"+a.Token) {
		t.Errorf("attachment = %+v, URL %q", a, a.URL())
	}
	got, err := e.svc.Get(context.Background(), a.Token)
	if err != nil || got.StorageKey != a.StorageKey || got.UploaderID == nil || *got.UploaderID != e.userID {
		t.Fatalf("Get = %+v, %v", got, err)
	}
	body, err := e.svc.Open(context.Background(), got)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = body.Close() }()
	data, _ := io.ReadAll(body)
	if !bytes.Equal(data, pngOf(t, color.NRGBA{200, 0, 0, 255})) {
		t.Error("stored bytes differ from the upload")
	}
}

func TestAttachmentService_RejectsNonImageWithoutWriting(t *testing.T) {
	e := newAttachmentEnv(t)
	_, err := e.svc.Upload(context.Background(), e.repoID, e.userID, strings.NewReader(`<svg xmlns="http://www.w3.org/2000/svg"/>`))
	if !errors.Is(err, attachment.ErrUnsupportedType) {
		t.Fatalf("err = %v, want ErrUnsupportedType", err)
	}
	var n int
	if err := e.db.QueryRow(`SELECT COUNT(*) FROM attachments WHERE repo_id = $1`, e.repoID).Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows after a rejected upload = %d, %v", n, err)
	}
}

func TestAttachmentService_FailedInsertDeletesObject(t *testing.T) {
	e := newAttachmentEnv(t)
	const missingUser = int64(1) << 60
	if _, err := e.svc.Upload(context.Background(), e.repoID, missingUser, bytes.NewReader(pngOf(t, color.NRGBA{1, 2, 3, 255}))); err == nil {
		t.Fatal("Upload by a missing user succeeded")
	}
	if _, err := e.svc.Get(context.Background(), strings.Repeat("0", 32)); !errors.Is(err, service.ErrAttachmentNotFound) {
		t.Fatalf("Get of an unknown token: %v", err)
	}
}

func TestAttachmentService_StorageUnconfigured(t *testing.T) {
	db := testutil.OpenTestDB(t)
	svcs := service.New(store.New(db), &config.Config{Git: config.GitConfig{ReposRoot: t.TempDir()}})
	_, err := svcs.Attachment.Upload(context.Background(), 1, 1, bytes.NewReader(pngOf(t, color.NRGBA{1, 1, 1, 255})))
	if !errors.Is(err, service.ErrStorageUnconfigured) {
		t.Fatalf("err = %v, want ErrStorageUnconfigured", err)
	}
}

func TestAttachmentService_Sweep(t *testing.T) {
	ctx := context.Background()
	const grace = 24 * time.Hour

	t.Run("deletes an old unreferenced attachment", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{1, 0, 0, 255})
		e.age(t, a, 48*time.Hour)
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if e.row(t, a) || e.exists(t, a.StorageKey) {
			t.Error("the row or object survived")
		}
	})

	t.Run("keeps a recent upload that nothing references yet", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{2, 0, 0, 255})
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if !e.row(t, a) || !e.exists(t, a.StorageKey) {
			t.Error("a draft's upload was swept inside the grace period")
		}
	})

	t.Run("keeps an old attachment an issue body references", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{3, 0, 0, 255})
		e.age(t, a, 48*time.Hour)
		testutil.Exec(t, e.db, `INSERT INTO issues (repo_id, number, author_id, title, body) VALUES ($1, 1, $2, 't', $3)`,
			e.repoID, e.userID, "see ![shot]("+a.URL()+")")
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if !e.row(t, a) || !e.exists(t, a.StorageKey) {
			t.Error("a referenced attachment was swept")
		}
	})

	t.Run("keeps an old attachment a comment references", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{4, 0, 0, 255})
		e.age(t, a, 48*time.Hour)
		var issueID int64
		if err := e.db.QueryRow(`INSERT INTO issues (repo_id, number, author_id, title) VALUES ($1, 1, $2, 't') RETURNING id`, e.repoID, e.userID).Scan(&issueID); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, e.db, `INSERT INTO comments (repo_id, issue_id, author_id, body) VALUES ($1, $2, $3, $4)`,
			e.repoID, issueID, e.userID, "![x]("+a.URL()+")")
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if !e.row(t, a) {
			t.Error("an attachment referenced by a comment was swept")
		}
	})

	t.Run("keeps an old attachment a project card description references", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{7, 0, 0, 255})
		e.age(t, a, 48*time.Hour)
		var columnID int64
		if err := e.db.QueryRow(`WITH p AS (INSERT INTO projects (repo_id, name) VALUES ($1, 'b') RETURNING id)
			INSERT INTO project_columns (project_id, name) SELECT id, 'c' FROM p RETURNING id`, e.repoID).Scan(&columnID); err != nil {
			t.Fatal(err)
		}
		testutil.Exec(t, e.db, `INSERT INTO project_cards (column_id, title, note) VALUES ($1, 't', $2)`, columnID, "![x]("+a.URL()+")")
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if !e.row(t, a) {
			t.Error("an attachment referenced by a card description was swept")
		}
	})

	t.Run("deletes it once the only reference is gone", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{5, 0, 0, 255})
		e.age(t, a, 48*time.Hour)
		testutil.Exec(t, e.db, `INSERT INTO issues (repo_id, number, author_id, title, body) VALUES ($1, 1, $2, 't', $3)`,
			e.repoID, e.userID, a.URL())
		testutil.Exec(t, e.db, `UPDATE issues SET body = 'edited out' WHERE repo_id = $1`, e.repoID)
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if e.row(t, a) || e.exists(t, a.StorageKey) {
			t.Error("an attachment nothing references survived")
		}
	})

	t.Run("deletes an attachment whose repo is gone, even a recent one", func(t *testing.T) {
		e := newAttachmentEnv(t)
		a := e.upload(t, color.NRGBA{6, 0, 0, 255})
		testutil.Exec(t, e.db, `DELETE FROM repositories WHERE id = $1`, e.repoID)
		if _, err := e.svc.Sweep(ctx, grace); err != nil {
			t.Fatal(err)
		}
		if e.row(t, a) || e.exists(t, a.StorageKey) {
			t.Error("an attachment of a deleted repo survived")
		}
	})
}

func TestAttachmentService_DeleteForRepo(t *testing.T) {
	e := newAttachmentEnv(t)
	a := e.upload(t, color.NRGBA{7, 0, 0, 255})
	b := e.upload(t, color.NRGBA{8, 0, 0, 255})
	if err := e.svc.DeleteForRepo(context.Background(), e.repoID); err != nil {
		t.Fatal(err)
	}
	for _, x := range []*model.Attachment{a, b} {
		if e.row(t, x) || e.exists(t, x.StorageKey) {
			t.Errorf("%s survived DeleteForRepo", x.Token)
		}
	}
}

func TestRepoService_PurgeExpiredDeletesAttachments(t *testing.T) {
	e := newAttachmentEnv(t)
	cfg := &config.Config{Git: config.GitConfig{ReposRoot: t.TempDir()}}
	svcs := service.New(store.New(e.db), cfg).WithStorage(e.backend)
	a, err := svcs.Attachment.Upload(context.Background(), e.repoID, e.userID, bytes.NewReader(pngOf(t, color.NRGBA{9, 0, 0, 255})))
	if err != nil {
		t.Fatal(err)
	}
	testutil.Exec(t, e.db, `UPDATE repositories SET deleted_at = NOW() - INTERVAL '31 days' WHERE id = $1`, e.repoID)

	if err := svcs.Repo.PurgeExpired(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := svcs.Attachment.Get(context.Background(), a.Token); !errors.Is(err, service.ErrAttachmentNotFound) {
		t.Errorf("attachment row survived the purge: %v", err)
	}
	if e.exists(t, a.StorageKey) {
		t.Error("attachment object survived the purge")
	}
}
