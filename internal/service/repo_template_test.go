package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func TestCreateFromTemplate_InvalidName_CreatesNothing(t *testing.T) {
	db := testutil.OpenTestDB(t)
	root := filepath.Join(t.TempDir(), "repos")
	owner := "tmpl_" + testutil.UniqueSuffix(t)
	ownerID := seedOwner(t, db, owner)
	tmplID := seedRepoRow(t, db, ownerID, owner, "tmpl", "NULL")
	testutil.Exec(t, db, `UPDATE repositories SET is_template = true WHERE id = $1`, tmplID)
	svc := newDiskRepoService(db, root)

	for _, name := range []string{"../../x", "a b"} {
		_, err := svc.CreateFromTemplate(context.Background(), tmplID, ownerID, owner, name, "")

		if !errors.Is(err, ErrInvalidRepoName) {
			t.Errorf("name %q: want ErrInvalidRepoName, got %v", name, err)
		}
		var rows int
		if err := db.QueryRowContext(context.Background(),
			`SELECT COUNT(*) FROM repositories WHERE owner_id = $1 AND name = $2`, ownerID, name,
		).Scan(&rows); err != nil {
			t.Fatal(err)
		}
		if rows != 0 {
			t.Errorf("name %q: no repository row may be created, found %d", name, rows)
		}
		if _, err := os.Stat(filepath.Join(root, owner, name+".git")); !os.IsNotExist(err) {
			t.Errorf("name %q: nothing may be created on disk; stat: %v", name, err)
		}
	}
}
