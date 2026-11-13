package service

import (
	"context"

	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// verifySignatures judges commits in one batch. The page renders whatever happens here, so a failed lookup leaves
// signed commits unverified instead of returning an error. The CodeService read methods carry no context, hence
// Background.
func (s *CodeService) verifySignatures(commits []*object.Commit) map[plumbing.Hash]SignatureResult {
	if s.signatures == nil {
		return nil
	}
	res, err := s.signatures.Verify(context.Background(), commits)
	if err == nil {
		return res
	}
	res = make(map[plumbing.Hash]SignatureResult, len(commits))
	for _, c := range commits {
		res[c.Hash] = SignatureResult{Status: SignatureUnverified, Reason: ReasonUnsupported, CommitterEmail: c.Committer.Email}
		if c.PGPSignature == "" {
			res[c.Hash] = SignatureResult{Status: SignatureUnsigned, CommitterEmail: c.Committer.Email}
		}
	}
	return res
}

// summarizeCommits is summarizeCommit for a list, with signatures verified in one batch.
func (s *CodeService) summarizeCommits(commits []*object.Commit) []CommitSummary {
	results := s.verifySignatures(commits)
	out := make([]CommitSummary, len(commits))
	for i, c := range commits {
		out[i] = summarizeCommit(c)
		out[i].Signature = results[c.Hash]
	}
	return out
}
