package seed

import (
	"fmt"
	"maps"
	"math/rand/v2"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/filemode"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

// historySpec describes one repository's backdated history. CodeService.CommitFile
// and the PR merge helpers stamp time.Now, so the seed writes objects itself.
type historySpec struct {
	Module      string
	Name        string
	Description string
	Langs       []*language // primary first
	Authors     []service.GitAuthor
	Start, End  time.Time
	MainCommits int
	Features    []featureSpec
	Tags        int
}

type featureSpec struct {
	Branch  string // generated from the first change when empty
	Author  int    // index into historySpec.Authors
	Commits int
	Merge   bool
}

type historyResult struct {
	MainTip  plumbing.Hash
	Features []featureResult // in historySpec.Features order
	Tags     []tagResult
}

type featureResult struct {
	featureSpec
	Tip   plumbing.Hash
	Title string
	Hunks []hunk
}

// hunk is a run of lines a branch added, numbered as in the branch tip's file.
type hunk struct {
	Path       string
	Start, End int
	Lines      []string
}

type tagResult struct {
	Name  string
	Notes []string
}

type gitWriter struct {
	repo    *gogit.Repository
	written map[plumbing.Hash]bool
}

type encoder interface {
	Encode(plumbing.EncodedObject) error
}

func (w *gitWriter) store(enc plumbing.EncodedObject) (plumbing.Hash, error) {
	h := enc.Hash()
	if w.written[h] {
		return h, nil
	}
	if _, err := w.repo.Storer.SetEncodedObject(enc); err != nil {
		return plumbing.ZeroHash, err
	}
	w.written[h] = true
	return h, nil
}

func (w *gitWriter) put(obj encoder) (plumbing.Hash, error) {
	enc := w.repo.Storer.NewEncodedObject()
	if err := obj.Encode(enc); err != nil {
		return plumbing.ZeroHash, err
	}
	return w.store(enc)
}

func (w *gitWriter) blob(content string) (plumbing.Hash, error) {
	enc := w.repo.Storer.NewEncodedObject()
	enc.SetType(plumbing.BlobObject)
	wr, err := enc.Writer()
	if err != nil {
		return plumbing.ZeroHash, err
	}
	if _, err := wr.Write([]byte(content)); err != nil {
		return plumbing.ZeroHash, err
	}
	if err := wr.Close(); err != nil {
		return plumbing.ZeroHash, err
	}
	return w.store(enc)
}

func (w *gitWriter) tree(files map[string]string) (plumbing.Hash, error) {
	entries := map[string]object.TreeEntry{}
	subdirs := map[string]map[string]string{}
	for p, content := range files {
		dir, rest, nested := strings.Cut(p, "/")
		if nested {
			if subdirs[dir] == nil {
				subdirs[dir] = map[string]string{}
			}
			subdirs[dir][rest] = content
			continue
		}
		h, err := w.blob(content)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries[p] = object.TreeEntry{Name: p, Mode: filemode.Regular, Hash: h}
	}
	for dir, sub := range subdirs {
		h, err := w.tree(sub)
		if err != nil {
			return plumbing.ZeroHash, err
		}
		entries[dir] = object.TreeEntry{Name: dir, Mode: filemode.Dir, Hash: h}
	}
	sorted := slices.Collect(maps.Values(entries))
	// Git orders a directory as if its name ended in "/".
	key := func(e object.TreeEntry) string {
		if e.Mode == filemode.Dir {
			return e.Name + "/"
		}
		return e.Name
	}
	sort.Slice(sorted, func(i, j int) bool { return key(sorted[i]) < key(sorted[j]) })
	return w.put(&object.Tree{Entries: sorted})
}

func (w *gitWriter) commit(s *repoState, parents []plumbing.Hash, author service.GitAuthor, when time.Time, msg string) (plumbing.Hash, error) {
	treeHash, err := w.tree(s.snapshot())
	if err != nil {
		return plumbing.ZeroHash, err
	}
	sig := object.Signature{Name: author.Name, Email: author.Email, When: when}
	return w.put(&object.Commit{Author: sig, Committer: sig, Message: msg + "\n", TreeHash: treeHash, ParentHashes: parents})
}

func (w *gitWriter) setRef(name plumbing.ReferenceName, h plumbing.Hash) error {
	return w.repo.Storer.SetReference(plumbing.NewHashReference(name, h))
}

type repoState struct {
	files map[string]*srcFile
}

func (s *repoState) clone() *repoState {
	c := &repoState{files: make(map[string]*srcFile, len(s.files))}
	for p, f := range s.files {
		c.files[p] = &srcFile{header: f.header, blocks: slices.Clone(f.blocks)}
	}
	return c
}

func (s *repoState) snapshot() map[string]string {
	out := make(map[string]string, len(s.files))
	for p, f := range s.files {
		out[p] = f.render()
	}
	return out
}

func (s *repoState) sourceFiles(lang *language) []string {
	var out []string
	for p := range s.files {
		if strings.HasSuffix(p, lang.Ext) {
			out = append(out, p)
		}
	}
	slices.Sort(out)
	return out
}

// appendBlock adds a block to an existing file of lang, or creates one, and returns the added lines.
func (s *repoState) appendBlock(rng *rand.Rand, lang *language, id identifier, newFile bool) (string, hunk) {
	existing := s.sourceFiles(lang)
	p := lang.fileName(id)
	if !newFile && len(existing) > 0 {
		p = pick(rng, existing)
	}
	f, ok := s.files[p]
	if !ok {
		f = &srcFile{header: lang.header}
		s.files[p] = f
	}
	before := lineCount(f.render())
	block := lang.block(rng, id)
	f.blocks = append(f.blocks, block)

	h := hunk{Path: p, Start: before + 1, Lines: strings.Split(strings.TrimSuffix(block, "\n"), "\n")}
	if !ok {
		h.Start = 1
		h.Lines = strings.Split(strings.TrimSuffix(f.render(), "\n"), "\n")
	} else if h.Lines[0] == "" {
		h.Start++
		h.Lines = h.Lines[1:]
	}
	h.End = h.Start + len(h.Lines) - 1

	msg := fmt.Sprintf("Add %s to %s", id.camel(), path.Base(p))
	if !ok {
		msg = "Add " + p
	}
	return msg, h
}

func (s *repoState) mutateMain(rng *rand.Rand, langs []*language) string {
	lang := langs[0]
	if len(langs) > 1 && chance(rng, 0.25) {
		lang = langs[1]
	}
	switch r := rng.Float64(); {
	case r < 0.15:
		readme := s.files["README.md"]
		id := ident(rng)
		readme.blocks = append(readme.blocks, fmt.Sprintf("\n## %s\n\nUse `%s` to %s.\n", capitalize(id.phrase()), id.camel(), id.phrase()))
		return pick(rng, []string{"Update README", "Document " + id.phrase(), "docs: explain " + id.noun + " handling"})
	case r < 0.30:
		for _, p := range s.sourceFiles(lang) {
			if f := s.files[p]; len(f.blocks) > 1 {
				f.blocks = f.blocks[:len(f.blocks)-1]
				return "Remove dead code from " + path.Base(p)
			}
		}
	case r < 0.45:
		msg, _ := s.appendBlock(rng, lang, ident(rng), true)
		return msg
	}
	msg, _ := s.appendBlock(rng, lang, ident(rng), false)
	return msg
}

func initialState(rng *rand.Rand, spec historySpec) *repoState {
	lang := spec.Langs[0]
	s := &repoState{files: map[string]*srcFile{
		"README.md":  {header: "# " + spec.Name + "\n\n" + spec.Description + "\n"},
		".gitignore": {header: lang.Gitignore},
	}}
	if mp, content := lang.manifest(spec.Module, rng); mp != "" {
		s.files[mp] = &srcFile{header: content}
	}
	s.appendBlock(rng, lang, ident(rng), true)
	return s
}

// sortedTimes returns n ascending times in [from, to), nudging most weekend picks onto weekdays.
func sortedTimes(rng *rand.Rand, from, to time.Time, n int) []time.Time {
	span := to.Sub(from)
	out := make([]time.Time, n)
	if span <= 0 {
		for i := range out {
			out[i] = from
		}
		return out
	}
	for i := range out {
		t := from.Add(time.Duration(rng.Int64N(int64(span))))
		for retry := 0; retry < 3 && (t.Weekday() == time.Saturday || t.Weekday() == time.Sunday); retry++ {
			t = from.Add(time.Duration(rng.Int64N(int64(span))))
		}
		out[i] = t.Truncate(time.Second)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
}

func featureTitle(rng *rand.Rand, id identifier) string {
	return fmt.Sprintf(pick(rng, []string{"Add %s helper", "Implement %s", "Support %s", "Refactor %s handling", "Speed up %s"}), id.phrase())
}

type branchNamer map[string]bool

func (seen branchNamer) name(rng *rand.Rand, id identifier) string {
	base := pick(rng, []string{"feature/", "feature/", "fix/", "chore/"}) + id.verb + "-" + id.noun
	name := base
	for i := 2; seen[name]; i++ {
		name = fmt.Sprintf("%s-%d", base, i)
	}
	seen[name] = true
	return name
}

// buildHistory writes spec's history into the bare repository at dir and points
// main, the feature branches and the tags at it.
func buildHistory(dir string, rng *rand.Rand, spec historySpec) (historyResult, error) {
	repo, err := gogit.PlainOpen(dir)
	if err != nil {
		return historyResult{}, err
	}
	w := &gitWriter{repo: repo, written: map[plumbing.Hash]bool{}}
	// Two commits give every merged feature a window to land in.
	n := max(spec.MainCommits, 2)
	ts := sortedTimes(rng, spec.Start, spec.End.Add(-time.Hour), n)
	names := branchNamer{}
	for _, f := range spec.Features {
		if f.Branch != "" {
			names[f.Branch] = true
		}
	}

	mergedAt := map[int][]int{}
	for i, f := range spec.Features {
		if f.Merge {
			k := rng.IntN(n - 1)
			mergedAt[k] = append(mergedAt[k], i)
		}
	}

	res := historyResult{Features: make([]featureResult, len(spec.Features))}
	buildBranch := func(i int, base *repoState, baseHash plumbing.Hash, from, to time.Time) (*repoState, error) {
		var err error
		f := spec.Features[i]
		st := base.clone()
		times := sortedTimes(rng, from, to, max(f.Commits, 1)+1)
		tip := baseHash
		fr := featureResult{featureSpec: f}
		for c := range max(f.Commits, 1) {
			id := ident(rng)
			msg, h := st.appendBlock(rng, spec.Langs[0], id, c == 0 && chance(rng, 0.6))
			if c == 0 {
				fr.Title = featureTitle(rng, id)
				msg = fr.Title
				if fr.Branch == "" {
					fr.Branch = names.name(rng, id)
				}
			}
			fr.Hunks = append(fr.Hunks, h)
			if tip, err = w.commit(st, []plumbing.Hash{tip}, spec.Authors[f.Author], times[c], msg); err != nil {
				return nil, err
			}
		}
		fr.Tip = tip
		res.Features[i] = fr
		return st, w.setRef(plumbing.NewBranchReferenceName(fr.Branch), tip)
	}

	state := initialState(rng, spec)
	mainHashes := make([]plumbing.Hash, n)
	stateAt := make([]*repoState, n)
	messages := make([][]string, n)
	var tip plumbing.Hash
	for k := range n {
		msg := "Initial commit"
		var parents []plumbing.Hash
		if k > 0 {
			msg = state.mutateMain(rng, spec.Langs)
			parents = []plumbing.Hash{tip}
		}
		author := spec.Authors[rng.IntN(len(spec.Authors))]
		if k == 0 {
			author = spec.Authors[0]
		}
		if tip, err = w.commit(state, parents, author, ts[k], msg); err != nil {
			return historyResult{}, err
		}
		mainHashes[k], stateAt[k] = tip, state.clone()
		messages[k] = append(messages[k], msg)

		window := len(mergedAt[k])
		for j, i := range mergedAt[k] {
			gap := ts[k+1].Sub(ts[k]) / time.Duration(window)
			from := ts[k].Add(gap*time.Duration(j) + time.Second)
			to := ts[k].Add(gap * time.Duration(j+1))
			branchState, err := buildBranch(i, state, tip, from, to)
			if err != nil {
				return historyResult{}, err
			}
			fr := res.Features[i]
			mergeMsg := fmt.Sprintf("Merge branch '%s'\n\n%s", fr.Branch, fr.Title)
			if tip, err = w.commit(branchState, []plumbing.Hash{tip, fr.Tip}, spec.Authors[0], to, mergeMsg); err != nil {
				return historyResult{}, err
			}
			state = branchState
			messages[k] = append(messages[k], fr.Title)
		}
	}
	res.MainTip = tip
	if err := w.setRef(plumbing.NewBranchReferenceName("main"), tip); err != nil {
		return historyResult{}, err
	}
	if err := repo.Storer.SetReference(plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))); err != nil {
		return historyResult{}, err
	}

	for i, f := range spec.Features {
		if f.Merge {
			continue
		}
		k := n - 1 - rng.IntN(max(n/3, 1))
		if _, err := buildBranch(i, stateAt[k], mainHashes[k], ts[k].Add(time.Second), spec.End); err != nil {
			return historyResult{}, err
		}
	}

	prev := -1
	for t := 1; t <= spec.Tags; t++ {
		k := t * (n - 1) / spec.Tags
		if k <= prev {
			continue
		}
		name := fmt.Sprintf("v0.%d.0", t)
		if t == spec.Tags && spec.Tags >= 3 {
			name = "v1.0.0"
		}
		var notes []string
		for _, ms := range messages[prev+1 : k+1] {
			notes = append(notes, ms...)
		}
		prev = k
		when := ts[k].Add(30 * time.Minute)
		sig := object.Signature{Name: spec.Authors[0].Name, Email: spec.Authors[0].Email, When: when}
		tagHash, err := w.put(&object.Tag{Name: name, Tagger: sig, Message: "Release " + name + "\n", TargetType: plumbing.CommitObject, Target: mainHashes[k]})
		if err != nil {
			return historyResult{}, err
		}
		if err := w.setRef(plumbing.NewTagReferenceName(name), tagHash); err != nil {
			return historyResult{}, err
		}
		res.Tags = append(res.Tags, tagResult{Name: name, Notes: notes[max(len(notes)-8, 0):]})
	}
	return res, nil
}
