package markdown

import (
	"context"
	"net/url"
	"path"
	"strings"

	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

// RepoLinks says where a repository file's relative links and images point.
type RepoLinks struct {
	// Blob and Raw are URL prefixes with no trailing slash; a link resolves under Blob, an image under Raw.
	Blob, Raw string
}

// RenderRepoFile is RenderCtx for a file at the repository root, resolving its relative links and images.
func RenderRepoFile(ctx context.Context, src string, links RepoLinks) string {
	return render(ctx, src, parser.WithASTTransformers(
		util.Prioritized[parser.ASTTransformer](&repoLinker{links: links}, 998),
	))
}

type repoLinker struct{ links RepoLinks }

func (t *repoLinker) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	source := reader.Source()
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Link:
			if dest, ok := resolveRepoPath(t.links.Blob, n.Destination.Value(source)); ok {
				n.Destination = text.NewSingleLineValueFromString(dest, text.IdentityDecoder)
			}
		case *ast.Image:
			if dest, ok := resolveRepoPath(t.links.Raw, n.Destination.Value(source)); ok {
				n.Destination = text.NewSingleLineValueFromString(dest, text.IdentityDecoder)
			}
		}
		return ast.WalkContinue, nil
	})
}

// resolveRepoPath maps a destination relative to the repository root onto prefix. Anything with a scheme or host, a
// bare fragment, and an empty destination are left alone. A path that climbs above the root stops at the root, as
// it does on the code tab.
func resolveRepoPath(prefix, dest string) (string, bool) {
	dest = strings.TrimSpace(dest)
	if dest == "" || strings.HasPrefix(dest, "#") || strings.HasPrefix(dest, "//") {
		return "", false
	}
	if u, err := url.Parse(dest); err != nil || u.Scheme != "" || u.Host != "" {
		return "", false
	}
	rel, suffix := dest, ""
	if i := strings.IndexAny(dest, "?#"); i >= 0 {
		rel, suffix = dest[:i], dest[i:]
	}
	return prefix + path.Clean("/"+rel) + suffix, true
}
