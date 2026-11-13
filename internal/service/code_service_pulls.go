package service

// PullCommits returns commits reachable from head but not from base, in
// reverse-chronological order.
func (s *CodeService) PullCommits(owner, repoName, base, head string) ([]CommitSummary, error) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return nil, err
	}
	baseCommit, _, err := resolveRef(repo, base)
	if err != nil {
		return nil, err
	}
	headCommit, _, err := resolveRef(repo, head)
	if err != nil {
		return nil, err
	}
	commits, err := commitRange(repo, baseCommit.Hash, headCommit.Hash)
	if err != nil {
		return nil, err
	}
	return s.summarizeCommits(commits), nil
}

// maxMergeScan bounds the first-parent walk that looks for a PR's merge commit.
const maxMergeScan = 20000

// ReconstructMergedRange rebuilds the base and head tips a merged PR saw, for a PR merged before they were recorded.
// It needs the head branch still at the tip that merged, so squash merges and moved or deleted branches are not
// recoverable. commits is the count the merge recorded, 0 when none was.
func (s *CodeService) ReconstructMergedRange(owner, repoName, base, head string, commits int) (baseSHA, headSHA string, ok bool) {
	repo, err := s.openRepo(owner, repoName)
	if err != nil {
		return "", "", false
	}
	baseTip, _, err := resolveRef(repo, base)
	if err != nil {
		return "", "", false
	}
	headTip, _, err := resolveRef(repo, head)
	if err != nil {
		return "", "", false
	}
	if landed, err := isAncestor(repo, headTip, baseTip); err != nil || !landed {
		return "", "", false
	}

	c := baseTip
	for range maxMergeScan {
		if len(c.ParentHashes) == 2 && c.ParentHashes[1] == headTip.Hash {
			return c.ParentHashes[0].String(), headTip.Hash.String(), true
		}
		if len(c.ParentHashes) == 0 {
			break
		}
		if c, err = c.Parent(0); err != nil {
			break
		}
	}

	// A fast-forward leaves no merge commit: the branch point is `commits` first parents below the head.
	if commits <= 0 {
		return "", "", false
	}
	c = headTip
	for range commits {
		if len(c.ParentHashes) == 0 {
			return "", "", false
		}
		if c, err = c.Parent(0); err != nil {
			return "", "", false
		}
	}
	if got, err := commitRange(repo, c.Hash, headTip.Hash); err != nil || len(got) != commits {
		return "", "", false
	}
	return c.Hash.String(), headTip.Hash.String(), true
}
