package service

import (
	"context"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// PushVetService makes the vet hook both transports give gittransport.NewServer, so HTTP and SSH cannot drift.
type PushVetService struct {
	protection *BranchProtectionService
	scan       *PushScanService
}

func NewPushVetService(protection *BranchProtectionService, scan *PushScanService) *PushVetService {
	return &PushVetService{protection: protection, scan: scan}
}

// PushVet vets the refs of one push: branch protection first, then the secret check.
type PushVet struct {
	ctx     context.Context
	repo    *model.Repository
	gitRepo *gogit.Repository
	svc     *PushVetService
	scan    *PushScan
}

// Begin starts the vet of one push by pusherID (0 for a deploy key).
func (s *PushVetService) Begin(ctx context.Context, repo *model.Repository, gitRepo *gogit.Repository, pusherID int64) *PushVet {
	return &PushVet{ctx: ctx, repo: repo, gitRepo: gitRepo, svc: s, scan: s.scan.Begin(repo, gitRepo, pusherID)}
}

// Expect names the push's commands, once decoded, so one scan covers every ref.
func (v *PushVet) Expect(cmds []*packp.Command) { v.scan.Expect(cmds) }

// Check is the vet hook for gittransport.NewServer.
func (v *PushVet) Check(cmd *packp.Command) error {
	if err := v.svc.protection.CheckPushCommand(v.ctx, v.repo.ID, v.gitRepo, cmd); err != nil {
		return err
	}
	return v.scan.Check(v.ctx, cmd)
}

// Scan is what PushService.AfterPush finishes once the refs have moved.
func (v *PushVet) Scan() *PushScan { return v.scan }
