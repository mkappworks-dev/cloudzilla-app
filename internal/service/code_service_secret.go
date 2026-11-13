package service

import (
	"errors"
	"strings"

	"github.com/go-git/go-git/v5/plumbing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
)

// ErrCommitGone means the commit or file an alert points at is no longer in the repository.
var ErrCommitGone = errors.New("commit no longer available")

const (
	snippetContext  = 3
	snippetMaxBytes = 1 << 20
	snippetMask     = "••••"

	secretTypePrivateKey = "private_key"
)

// SecretSnippetLine is one masked line. Before, Mask and After split Text around the first masked span, which Mask
// holds; Mask is empty on a line that has none.
type SecretSnippetLine struct {
	Number              int
	Text                string
	Secret              bool
	Before, Mask, After string
}

type SecretSnippet struct{ Lines []SecretSnippetLine }

// SecretSnippet reads the alert's location from the commit and masks the secret: the full value never leaves this
// function. The lines after a private key header are dropped because they are the key.
func (s *CodeService) SecretSnippet(owner, repoName string, alert *model.SecretScanAlert, loc model.SecretScanLocation) (*SecretSnippet, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	commit, err := repo.CommitObject(plumbing.NewHash(loc.CommitSHA))
	if err != nil {
		return nil, ErrCommitGone
	}
	f, err := commit.File(loc.FilePath)
	if err != nil || f.Size > snippetMaxBytes {
		return nil, ErrCommitGone
	}
	text, err := f.Contents()
	if err != nil {
		return nil, ErrCommitGone
	}

	value := findSecretValue(alert, loc.FilePath, text, loc.LineNumber)
	// A private key's value spans lines; only its header line is ever shown.
	shown, _, _ := strings.Cut(value, "\n")
	shown = strings.TrimSuffix(shown, "\r")
	lines := strings.Split(text, "\n")
	if loc.LineNumber < 1 || loc.LineNumber > len(lines) || shown == "" {
		return nil, ErrCommitGone
	}

	after := snippetContext
	if alert.SecretType == secretTypePrivateKey {
		after = 0
	}
	from := max(loc.LineNumber-snippetContext, 1)
	to := min(loc.LineNumber+after, len(lines))
	out := &SecretSnippet{}
	for n := from; n <= to; n++ {
		line := strings.TrimSuffix(lines[n-1], "\r")
		// Context lines can repeat the value, so every line is masked, not only the finding's.
		mask := maskedSpan(alert)
		line = strings.ReplaceAll(line, shown, mask)
		l := SecretSnippetLine{Number: n, Text: line, Secret: n == loc.LineNumber, Before: line}
		if i := strings.Index(line, mask); i >= 0 {
			l.Before, l.Mask, l.After = line[:i], mask, line[i+len(mask):]
		}
		out.Lines = append(out.Lines, l)
	}
	return out, nil
}

// findSecretValue re-scans the file for the finding the alert's hash names, since only the hash is stored.
func findSecretValue(alert *model.SecretScanAlert, path, text string, line int) string {
	for _, f := range secretscan.Scan(path, []byte(text), snippetMaxBytes) {
		if f.Line == line && f.Type == alert.SecretType && secretscan.Hash(alert.RepoID, f.Value) == alert.SecretHash {
			return f.Value
		}
	}
	return ""
}

func maskedSpan(alert *model.SecretScanAlert) string {
	hint := []rune(alert.SecretHint)
	if alert.SecretType == secretTypePrivateKey || len(hint) != 8 {
		return snippetMask
	}
	return string(hint[:4]) + snippetMask + string(hint[4:])
}

// CommitOnBranch reports whether branch's tip reaches sha. A missing branch or commit is false, not an error.
func (s *CodeService) CommitOnBranch(owner, repoName, sha, branch string) (bool, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return false, err
	}
	tip, err := branchTipCommit(repo, branch)
	if err != nil {
		return false, nil
	}
	c, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return false, nil
	}
	return isAncestor(repo, c, tip)
}
