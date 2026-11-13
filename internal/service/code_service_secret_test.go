package service

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	czconfig "github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

const snippetKey = "AKIAIOSFODNN7EXAMPLE"

func newSnippetRepo(t *testing.T, content string) (*CodeService, string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "ada", "vault.git")
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		t.Fatal(err)
	}
	testutil.InitBareRepo(t, dir)
	svc := NewCodeService(czconfig.GitConfig{ReposRoot: root})
	if _, err := svc.CommitFile("ada", "vault", "main", "config/deploy.env", []byte(content), GitAuthor{Name: "Ada", Email: "ada@example.test"}, "Add deploy.env"); err != nil {
		t.Fatalf("commit: %v", err)
	}
	commit, _, err := svc.ResolveRef("ada", "vault", "main")
	if err != nil {
		t.Fatal(err)
	}
	return svc, commit.Hash.String()
}

func snippetAlert(repoID int64) *model.SecretScanAlert {
	return &model.SecretScanAlert{
		RepoID: repoID, SecretType: "aws_access_key_id",
		SecretHash: secretscan.Hash(repoID, snippetKey), SecretHint: secretscan.Mask(snippetKey),
	}
}

func TestSecretSnippet_MasksTheSecretAndKeepsContext(t *testing.T) {
	content := "# staging\nHOST=staging\nREGION=eu\nAWS_ACCESS_KEY_ID=" + snippetKey + "\nSECRET=${AWS_SECRET}\nBUCKET=b\nEXTRA=1\nLAST=2\n"
	svc, sha := newSnippetRepo(t, content)

	got, err := svc.SecretSnippet("ada", "vault", snippetAlert(7), model.SecretScanLocation{CommitSHA: sha, FilePath: "config/deploy.env", LineNumber: 4})
	if err != nil {
		t.Fatalf("SecretSnippet: %v", err)
	}
	if len(got.Lines) != 7 || got.Lines[0].Number != 1 || got.Lines[6].Number != 7 {
		t.Fatalf("want lines 1..7 (3 of context each side), got %+v", got.Lines)
	}
	for _, l := range got.Lines {
		if strings.Contains(l.Text, snippetKey) {
			t.Fatalf("line %d holds the full value: %q", l.Number, l.Text)
		}
		if l.Secret != (l.Number == 4) {
			t.Errorf("line %d Secret = %v", l.Number, l.Secret)
		}
	}
	if want := "AWS_ACCESS_KEY_ID=AKIA••••MPLE"; got.Lines[3].Text != want {
		t.Errorf("secret line = %q, want %q", got.Lines[3].Text, want)
	}
}

func TestSecretSnippet_PrivateKeyShowsNoKeyBody(t *testing.T) {
	content := "# key\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA\nQQQQ\n"
	svc, sha := newSnippetRepo(t, content)
	alert := &model.SecretScanAlert{RepoID: 7, SecretType: "private_key",
		SecretHash: secretscan.Hash(7, "-----BEGIN OPENSSH PRIVATE KEY-----"), SecretHint: "OPENSSH"}

	got, err := svc.SecretSnippet("ada", "vault", alert, model.SecretScanLocation{CommitSHA: sha, FilePath: "config/deploy.env", LineNumber: 2})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range got.Lines {
		if strings.Contains(l.Text, "b3BlbnNz") || strings.Contains(l.Text, "OPENSSH PRIVATE KEY") {
			t.Errorf("line %d leaks key material: %q", l.Number, l.Text)
		}
	}
	if len(got.Lines) != 2 {
		t.Errorf("want the line before and the header only, got %d lines", len(got.Lines))
	}
}

func TestSecretSnippet_PrunedCommit(t *testing.T) {
	svc, _ := newSnippetRepo(t, "A="+snippetKey+"\n")
	_, err := svc.SecretSnippet("ada", "vault", snippetAlert(7), model.SecretScanLocation{CommitSHA: strings.Repeat("ab", 20), FilePath: "config/deploy.env", LineNumber: 1})
	if !errors.Is(err, ErrCommitGone) {
		t.Fatalf("err = %v, want ErrCommitGone", err)
	}
}

func TestCommitOnBranch(t *testing.T) {
	svc, sha := newSnippetRepo(t, "A=1\n")
	if ok, err := svc.CommitOnBranch("ada", "vault", sha, "main"); err != nil || !ok {
		t.Errorf("tip on main = %v, %v", ok, err)
	}
	if ok, err := svc.CommitOnBranch("ada", "vault", strings.Repeat("ab", 20), "main"); err != nil || ok {
		t.Errorf("unknown commit = %v, %v", ok, err)
	}
	if ok, err := svc.CommitOnBranch("ada", "vault", sha, "nope"); err != nil || ok {
		t.Errorf("missing branch = %v, %v", ok, err)
	}
}
