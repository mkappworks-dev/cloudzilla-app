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
	var out []CommitSummary
	for _, c := range commits {
		out = append(out, summarizeCommit(c))
	}
	return out, nil
}
