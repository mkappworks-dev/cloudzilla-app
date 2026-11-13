package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/secretscan"
)

// CheckWebContent applies push protection to content about to be committed at path on branch from the browser. The
// commit does not exist yet, so a blocked alert has no commit to name. It returns a *SecretDetectedError to refuse,
// ErrSecretCheckFailed when a blocked alert cannot be written, and nil otherwise, including when the scan or a lookup
// cannot finish: the commit is then scanned afterwards by AfterWebCommit.
func (s *PushScanService) CheckWebContent(ctx context.Context, repo *model.Repository, branch, path string, content []byte, actorID int64) error {
	if !s.cfg.PushProtection {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.PushTimeout)
	defer cancel()
	findings, err := s.scan(path, content, s.cfg.MaxBlobBytes)
	if err != nil || ctx.Err() != nil {
		slog.Warn("push-protection: web scan incomplete", "repo_id", repo.ID, "error", err)
		return nil
	}
	p := &PushScan{svc: s, repo: repo, pusherID: actorID, recorded: make(map[string]*model.SecretScanAlert),
		revokedHashes: make(map[string]bool)}
	cmd := &packp.Command{Name: plumbing.NewBranchReferenceName(branch)}
	var blocking []blockedFinding
	for _, f := range findings {
		if f.Confidence != secretscan.High {
			continue
		}
		pf := pathFinding{Finding: f, Path: path}
		if block, revoked := p.blocks(ctx, cmd, plumbing.ZeroHash, pf); block {
			blocking = append(blocking, blockedFinding{pathFinding: pf, revoked: revoked})
		}
	}
	if len(blocking) == 0 {
		return nil
	}
	var first *model.SecretScanAlert
	for _, b := range blocking {
		alert, err := p.recordBlocked(ctx, cmd, b)
		if err != nil {
			return err
		}
		if first == nil {
			first = alert
		}
	}
	return &SecretDetectedError{
		Type: first.SecretType, Path: path, Line: blocking[0].Line, More: len(blocking) - 1, Revoked: blocking[0].revoked,
		URL: fmt.Sprintf("%s/%s/%s/security/secrets/%d", s.baseURL, repo.OwnerName, repo.Name, first.Number),
	}
}
