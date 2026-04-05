package service

import (
	"fmt"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	gogitdiff "github.com/go-git/go-git/v5/plumbing/format/diff"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/storer"
)

// CommitSummary holds abbreviated commit metadata for list views.
type CommitSummary struct {
	Hash       string
	FullHash   string
	Message    string
	Author     string
	AuthorTime time.Time
}

// CommitLog holds a paginated list of commits for a ref.
type CommitLog struct {
	Commits  []CommitSummary
	Ref      string
	Page     int
	PrevPage int
	NextPage int
	HasMore  bool
}

// DiffLine represents one line in a unified diff with type (add/del/ctx) and line numbers.
type DiffLine struct {
	Type    string // "add", "del", "ctx"
	Content string
	OldNum  int
	NewNum  int
}

// DiffHunk groups contiguous diff lines under a unified diff hunk header.
type DiffHunk struct {
	Header string
	Lines  []DiffLine
}

// FileDiff holds the complete diff for a single file including all hunks and stats.
type FileDiff struct {
	OldPath  string
	NewPath  string
	IsBinary bool
	IsNew    bool
	IsDelete bool
	Added    int
	Deleted  int
	Hunks    []DiffHunk
}

// CommitDetail holds full metadata and file diffs for a single commit.
type CommitDetail struct {
	Hash         string
	FullHash     string
	Message      string
	Body         string
	Author       string
	AuthorEmail  string
	AuthorTime   time.Time
	ParentHashes []string
	Files        []FileDiff
	TotalAdded   int
	TotalDeleted int
}

// GetCommits returns a paginated commit log for the given ref.
func (s *CodeService) GetCommits(owner, repoName, ref string, page, pageSize int) (*CommitLog, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}
	commit, displayRef, err := resolveRef(repo, ref)
	if err != nil {
		return nil, err
	}

	iter, err := repo.Log(&gogit.LogOptions{From: commit.Hash, Order: gogit.LogOrderCommitterTime})
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	skip := (page - 1) * pageSize
	var commits []CommitSummary
	count := 0

	err = iter.ForEach(func(c *object.Commit) error {
		if count < skip {
			count++
			return nil
		}
		if len(commits) > pageSize {
			return storer.ErrStop
		}
		hash := c.Hash.String()
		shortHash := hash
		if len(hash) > 7 {
			shortHash = hash[:7]
		}
		msg := strings.TrimSpace(c.Message)
		firstLine := msg
		if idx := strings.Index(msg, "\n"); idx >= 0 {
			firstLine = strings.TrimSpace(msg[:idx])
		}
		commits = append(commits, CommitSummary{
			Hash:       shortHash,
			FullHash:   hash,
			Message:    firstLine,
			Author:     c.Author.Name,
			AuthorTime: c.Author.When,
		})
		count++
		return nil
	})
	if err != nil {
		return nil, err
	}

	hasMore := len(commits) > pageSize
	if hasMore {
		commits = commits[:pageSize]
	}

	prevPage := 0
	if page > 1 {
		prevPage = page - 1
	}
	nextPage := 0
	if hasMore {
		nextPage = page + 1
	}

	return &CommitLog{
		Commits:  commits,
		Ref:      displayRef,
		Page:     page,
		PrevPage: prevPage,
		NextPage: nextPage,
		HasMore:  hasMore,
	}, nil
}

// GetCommit returns a single commit with full diff.
func (s *CodeService) GetCommit(owner, repoName, sha string) (*CommitDetail, error) {
	repo, err := gogit.PlainOpen(s.repoPath(owner, repoName))
	if err != nil {
		return nil, err
	}

	commit, err := repo.CommitObject(plumbing.NewHash(sha))
	if err != nil {
		return nil, err
	}

	shortHash := sha
	if len(sha) > 7 {
		shortHash = sha[:7]
	}

	msg := strings.TrimSpace(commit.Message)
	firstLine := msg
	body := ""
	if idx := strings.Index(msg, "\n"); idx >= 0 {
		firstLine = strings.TrimSpace(msg[:idx])
		body = strings.TrimSpace(msg[idx+1:])
	}

	var parentHashes []string
	for _, ph := range commit.ParentHashes {
		h := ph.String()
		if len(h) > 7 {
			h = h[:7]
		}
		parentHashes = append(parentHashes, h)
	}

	commitTree, err := commit.Tree()
	if err != nil {
		return nil, err
	}

	var patch *object.Patch
	if commit.NumParents() == 0 {
		changes, err := object.DiffTree(nil, commitTree)
		if err != nil {
			return nil, err
		}
		patch, err = changes.Patch()
		if err != nil {
			return nil, err
		}
	} else {
		parent, err := commit.Parent(0)
		if err != nil {
			return nil, err
		}
		patch, err = parent.Patch(commit)
		if err != nil {
			return nil, err
		}
	}

	var files []FileDiff
	totalAdded, totalDeleted := 0, 0

	for _, fp := range patch.FilePatches() {
		from, to := fp.Files()
		var oldPath, newPath string
		if from != nil {
			oldPath = from.Path()
		}
		if to != nil {
			newPath = to.Path()
		}
		isNew := from == nil
		isDelete := to == nil
		if isDelete && newPath == "" {
			newPath = oldPath
		}
		if isNew && oldPath == "" {
			oldPath = newPath
		}

		fd := FileDiff{
			OldPath:  oldPath,
			NewPath:  newPath,
			IsBinary: fp.IsBinary(),
			IsNew:    isNew,
			IsDelete: isDelete,
		}
		if !fp.IsBinary() {
			fd.Hunks = buildHunks(fp.Chunks())
			for _, h := range fd.Hunks {
				for _, l := range h.Lines {
					switch l.Type {
					case "add":
						fd.Added++
					case "del":
						fd.Deleted++
					}
				}
			}
		}
		totalAdded += fd.Added
		totalDeleted += fd.Deleted
		files = append(files, fd)
	}

	return &CommitDetail{
		Hash:         shortHash,
		FullHash:     commit.Hash.String(),
		Message:      firstLine,
		Body:         body,
		Author:       commit.Author.Name,
		AuthorEmail:  commit.Author.Email,
		AuthorTime:   commit.Author.When,
		ParentHashes: parentHashes,
		Files:        files,
		TotalAdded:   totalAdded,
		TotalDeleted: totalDeleted,
	}, nil
}

func buildHunks(chunks []gogitdiff.Chunk) []DiffHunk {
	const ctx = 3

	type lineInfo struct {
		op      gogitdiff.Operation
		content string
		oldNum  int
		newNum  int
	}
	var lines []lineInfo
	oldN, newN := 1, 1
	for _, chunk := range chunks {
		content := strings.TrimSuffix(chunk.Content(), "\n")
		parts := strings.Split(content, "\n")
		for _, part := range parts {
			switch chunk.Type() {
			case gogitdiff.Equal:
				lines = append(lines, lineInfo{gogitdiff.Equal, part, oldN, newN})
				oldN++
				newN++
			case gogitdiff.Add:
				lines = append(lines, lineInfo{gogitdiff.Add, part, 0, newN})
				newN++
			case gogitdiff.Delete:
				lines = append(lines, lineInfo{gogitdiff.Delete, part, oldN, 0})
				oldN++
			}
		}
	}

	include := make([]bool, len(lines))
	for i, l := range lines {
		if l.op != gogitdiff.Equal {
			lo, hi := max(0, i-ctx), min(len(lines)-1, i+ctx)
			for j := lo; j <= hi; j++ {
				include[j] = true
			}
		}
	}

	var hunks []DiffHunk
	for i := 0; i < len(lines); {
		if !include[i] {
			i++
			continue
		}
		var hunkLines []DiffLine
		for i < len(lines) && include[i] {
			l := lines[i]
			opMap := map[gogitdiff.Operation]string{
				gogitdiff.Add:    "add",
				gogitdiff.Delete: "del",
				gogitdiff.Equal:  "ctx",
			}
			hunkLines = append(hunkLines, DiffLine{opMap[l.op], l.content, l.oldNum, l.newNum})
			i++
		}
		oldStart, newStart, oldCount, newCount := 0, 0, 0, 0
		for _, hl := range hunkLines {
			if hl.Type != "add" {
				oldCount++
				if oldStart == 0 && hl.OldNum > 0 {
					oldStart = hl.OldNum
				}
			}
			if hl.Type != "del" {
				newCount++
				if newStart == 0 && hl.NewNum > 0 {
					newStart = hl.NewNum
				}
			}
		}
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@", oldStart, oldCount, newStart, newCount)
		hunks = append(hunks, DiffHunk{Header: header, Lines: hunkLines})
	}
	return hunks
}
