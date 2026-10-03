package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	gogit "github.com/go-git/go-git/v5"
	gitconfig "github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/transport"
)

// defaultImportBranch prefers the source's HEAD target, then main, then the
// first branch by name.
func defaultImportBranch(refs []*plumbing.Reference) (string, error) {
	var head plumbing.ReferenceName
	branches := map[plumbing.ReferenceName]bool{}
	for _, ref := range refs {
		switch {
		case ref.Name() == plumbing.HEAD && ref.Type() == plumbing.SymbolicReference:
			head = ref.Target()
		case ref.Name().IsBranch():
			branches[ref.Name()] = true
		}
	}
	if len(branches) == 0 {
		return "", ErrImportEmptySource
	}
	if branches[head] {
		return head.Short(), nil
	}
	if main := plumbing.NewBranchReferenceName("main"); branches[main] {
		return main.Short(), nil
	}
	names := make([]string, 0, len(branches))
	for name := range branches {
		names = append(names, name.Short())
	}
	sort.Strings(names)
	return names[0], nil
}

// cloneForImport fetches src's branches and tags into a new bare repo at dir,
// points HEAD at the source's default branch and returns that branch.
func cloneForImport(ctx context.Context, dir, src string, auth transport.AuthMethod, progress io.Writer) (string, error) {
	repo, err := gogit.PlainInit(dir, true)
	if err != nil {
		return "", fmt.Errorf("init bare repo: %w", err)
	}
	remote, err := repo.CreateRemote(&gitconfig.RemoteConfig{
		Name: "origin",
		URLs: []string{src},
		// Not a mirror's +refs/*:refs/*, which would also copy GitHub's refs/pull/*.
		Fetch: []gitconfig.RefSpec{"+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"},
	})
	if err != nil {
		return "", fmt.Errorf("add remote: %w", err)
	}
	refs, err := remote.ListContext(ctx, &gogit.ListOptions{Auth: auth})
	if errors.Is(err, transport.ErrEmptyRemoteRepository) {
		return "", ErrImportEmptySource
	}
	if err != nil {
		return "", err
	}
	branch, err := defaultImportBranch(refs)
	if err != nil {
		return "", err
	}
	err = remote.FetchContext(ctx, &gogit.FetchOptions{Auth: auth, Tags: gogit.NoTags, Progress: progress})
	if err != nil && !errors.Is(err, gogit.NoErrAlreadyUpToDate) {
		return "", err
	}
	head := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName(branch))
	if err := repo.Storer.SetReference(head); err != nil {
		return "", fmt.Errorf("set HEAD: %w", err)
	}
	if err := repo.DeleteRemote("origin"); err != nil {
		return "", fmt.Errorf("remove remote: %w", err)
	}
	return branch, nil
}
