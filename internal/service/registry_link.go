package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	sourceLabel    = "org.opencontainers.image.source"
	maxConfigBytes = 1 << 20
)

// errLinkRaced means a concurrent push linked the image name first.
var errLinkRaced = errors.New("image name linked by a concurrent push")

func linkRefusal(owner, reason string) *RegistryError {
	msg := fmt.Sprintf("this image is not linked to a repository: push it to a path named like a repository of %[1]s "+
		"(%[1]s/<repository>) that you can write to, or set the %[2]s label to a repository of %[1]s that you manage "+
		"(owner/repo, or its URL on this server)", owner, sourceLabel)
	if reason != "" {
		msg = reason + "; " + msg
	}
	return regErr(http.StatusForbidden, "DENIED", msg)
}

// linkPending links a package created by a blob upload to a Repository at its first manifest push. A Repository named
// like the image's first path segment always wins, so a label can't claim a name that belongs to a Repository.
func (s *RegistryService) linkPending(ctx context.Context, userID int64, t uploadTarget, name string, doc *manifestDoc) error {
	owner, image, err := s.resolveOwner(ctx, name)
	if err != nil {
		return err
	}
	repos, err := s.repos.ListByOwner(ctx, owner.Name)
	if err != nil {
		return err
	}
	find := func(repoName string) *model.Repository {
		for i := range repos {
			if strings.EqualFold(repos[i].Name, repoName) {
				return &repos[i]
			}
		}
		return nil
	}
	first, _, _ := strings.Cut(image, "/")
	if repo := find(first); repo != nil {
		if !s.repos.CanWrite(ctx, repo, userID) {
			return errDenied
		}
		return s.link(ctx, t, repo.ID)
	}

	label, err := s.readSourceLabel(ctx, t.packageID, owner.Name, doc)
	if err != nil {
		return err
	}
	labelOwner, labelRepo, ok := s.parseSourceLabel(label)
	if !ok || !strings.EqualFold(labelOwner, owner.Name) {
		return linkRefusal(owner.Name, "")
	}
	repo := find(labelRepo)
	if repo == nil || !s.repos.CanManage(ctx, repo, userID) {
		return linkRefusal(owner.Name, "")
	}
	return s.link(ctx, t, repo.ID)
}

func (s *RegistryService) link(ctx context.Context, t uploadTarget, repoID int64) error {
	err := s.registry.LinkPackage(ctx, t.packageID, repoID)
	if errors.Is(err, store.ErrImageNameTaken) || errors.Is(err, sql.ErrNoRows) {
		return errLinkRaced
	}
	return err
}

// readSourceLabel returns the image config's source label, or "" when there is none. A config over 1 MiB is
// refused unread: it only matters here, so a name-matched push never pays for it.
func (s *RegistryService) readSourceLabel(ctx context.Context, packageID int64, ownerName string, doc *manifestDoc) (string, error) {
	if doc.Config == nil {
		return "", nil
	}
	size, err := s.registry.BlobSize(ctx, packageID, doc.Config.Digest)
	if err != nil {
		return "", err
	}
	if size > maxConfigBytes {
		return "", linkRefusal(ownerName, "the image config is larger than 1 MiB, so its label can't be read")
	}
	rc, err := s.backend.Get(ctx, blobKey(doc.Config.Digest))
	if err != nil {
		return "", fmt.Errorf("read image config: %w", err)
	}
	defer func() { _ = rc.Close() }()
	raw, err := io.ReadAll(io.LimitReader(rc, maxConfigBytes+1))
	if err != nil {
		return "", fmt.Errorf("read image config: %w", err)
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if len(raw) > maxConfigBytes || json.Unmarshal(raw, &cfg) != nil {
		return "", nil
	}
	return cfg.Config.Labels[sourceLabel], nil
}

// parseSourceLabel reads "owner/repo" or a URL on this server's host, leniently: a ".git" suffix, a trailing "/",
// a path below the repository and the host's case are all fine. Another host's URL is not ours to resolve.
func (s *RegistryService) parseSourceLabel(label string) (owner, repo string, ok bool) {
	label = strings.TrimSpace(label)
	path, exact := label, true
	if strings.Contains(label, "://") {
		u, err := url.Parse(label)
		if err != nil || !strings.EqualFold(u.Host, s.service) {
			return "", "", false
		}
		path, exact = u.Path, false
	}
	var segs []string
	for _, seg := range strings.Split(path, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) < 2 || (exact && len(segs) != 2) {
		return "", "", false
	}
	return segs[0], strings.TrimSuffix(segs[1], ".git"), true
}
