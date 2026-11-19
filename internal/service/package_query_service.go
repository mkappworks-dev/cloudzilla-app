package service

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mkappworks-dev/cloudzilla-app/internal/adminmode"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

var (
	ErrPackageForbidden = errors.New("not allowed to change this package")
	ErrRelinkTarget     = errors.New("a package can only be linked to a repository of its path owner")
)

// WithAccess gives the service what the read, delete and re-link rules ask permissions of.
func (s *PackageService) WithAccess(repos *RepoService, orgs *OrgService) *PackageService {
	s.repos, s.orgs = repos, orgs
	return s
}

// ListByRepo returns the packages linked to repo, or none when viewerID cannot read it.
func (s *PackageService) ListByRepo(ctx context.Context, repo *model.Repository, viewerID *int64) ([]model.PackageSummary, error) {
	if !s.repos.CanRead(ctx, repo, viewerID) {
		return nil, nil
	}
	return s.packages.ListByRepo(ctx, repo.ID)
}

// ListByOwner returns the linked packages under the path owner ownerName that viewerID may read, whatever
// Repository each now sits in. A pending package is never listed. The path owner also gets the packages sitting in
// Repositories they cannot read, flagged RepoHidden, so they can still delete or re-link them.
func (s *PackageService) ListByOwner(ctx context.Context, ownerName string, viewerID *int64) ([]model.PackageSummary, error) {
	owner, err := s.packages.ResolveOwner(ctx, ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	all, err := s.packages.ListByPathOwner(ctx, owner.UserID, owner.OrgID)
	if err != nil {
		return nil, err
	}
	manages := viewerID != nil && s.ownerIsPathOwner(ctx, owner, *viewerID)
	type access struct{ readable, private bool }
	seen := map[int64]access{}
	out := all[:0]
	for _, p := range all {
		a, ok := seen[*p.RepoID]
		if !ok {
			repo, err := s.repos.GetByID(ctx, *p.RepoID)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return nil, err
			}
			if err == nil {
				a = access{readable: s.repos.CanRead(ctx, repo, viewerID), private: repo.Private}
			}
			seen[*p.RepoID] = a
		}
		if !a.readable && !manages {
			continue
		}
		p.RepoPrivate, p.RepoHidden = a.private, !a.readable
		out = append(out, p)
	}
	return out, nil
}

// InOwner returns the package with id under ownerName's path, or ErrPackageNotFound, also when viewerID cannot see it.
func (s *PackageService) InOwner(ctx context.Context, ownerName string, viewerID *int64, id int64) (*model.PackageSummary, error) {
	list, err := s.ListByOwner(ctx, ownerName, viewerID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, ErrPackageNotFound
}

// IsPathOwner reports whether userID may manage every package under ownerName's path.
func (s *PackageService) IsPathOwner(ctx context.Context, ownerName string, userID int64) bool {
	owner, err := s.packages.ResolveOwner(ctx, ownerName)
	return err == nil && s.ownerIsPathOwner(ctx, owner, userID)
}

func (s *PackageService) ownerIsPathOwner(ctx context.Context, o store.PathOwner, userID int64) bool {
	p := &model.Package{}
	if o.OrgID != 0 {
		p.OwnerOrgID = &o.OrgID
	} else {
		p.OwnerUserID = &o.UserID
	}
	return s.isPathOwner(ctx, p, userID)
}

// ContainerDetail is a container package with what its page lists.
type ContainerDetail struct {
	Package  model.Package
	Repo     *model.Repository
	Tags     []model.TagRow
	Untagged []model.ManifestRow
}

// ContainerDetail returns ErrPackageNotFound for a missing package and for one viewerID cannot read.
func (s *PackageService) ContainerDetail(ctx context.Context, ownerName, name string, viewerID *int64) (*ContainerDetail, error) {
	owner, err := s.packages.ResolveOwner(ctx, ownerName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPackageNotFound
	}
	if err != nil {
		return nil, err
	}
	p, err := s.packages.LinkedPackage(ctx, owner.UserID, owner.OrgID, model.PackageTypeContainer, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrPackageNotFound
	}
	if err != nil {
		return nil, err
	}
	repo, err := s.repos.GetByID(ctx, *p.RepoID)
	if errors.Is(err, sql.ErrNoRows) || err == nil && !s.repos.CanRead(ctx, repo, viewerID) {
		return nil, ErrPackageNotFound
	}
	if err != nil {
		return nil, err
	}
	d := &ContainerDetail{Package: *p, Repo: repo}
	if d.Tags, err = s.packages.Tags(ctx, p.ID); err != nil {
		return nil, err
	}
	if d.Untagged, err = s.packages.UntaggedManifests(ctx, p.ID); err != nil {
		return nil, err
	}
	return d, nil
}

// Package returns a linked package, or ErrPackageNotFound.
func (s *PackageService) Package(ctx context.Context, id int64) (*model.Package, error) {
	p, err := s.packages.PackageByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) || err == nil && p.RepoID == nil {
		return nil, ErrPackageNotFound
	}
	return p, err
}

// isPathOwner is true for the user a path starts with, for an owner of the organization it starts with, and for a
// superadmin in admin mode.
func (s *PackageService) isPathOwner(ctx context.Context, p *model.Package, userID int64) bool {
	if p.OwnerUserID != nil && *p.OwnerUserID == userID {
		return true
	}
	if p.OwnerOrgID != nil && s.orgs.IsOwner(ctx, *p.OwnerOrgID, userID) {
		return true
	}
	return adminmode.Override(ctx, userID)
}

// Delete removes a whole package: its rows, and the objects of its generic files. A container package's blobs stay
// for the sweep, which deletes each once no package links it. It needs CanManage on the linked Repository or path
// ownership, which survives a transfer.
func (s *PackageService) Delete(ctx context.Context, actorID, packageID int64) error {
	p, err := s.Package(ctx, packageID)
	if err != nil {
		return err
	}
	repo, err := s.repos.GetByID(ctx, *p.RepoID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if !s.isPathOwner(ctx, p, actorID) && (repo == nil || !s.repos.CanManage(ctx, repo, actorID)) {
		return ErrPackageForbidden
	}
	keys, err := s.packages.DeletePackage(ctx, packageID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrPackageNotFound
	}
	if err != nil {
		return err
	}
	for _, k := range keys {
		s.deleteObject(ctx, k)
	}
	return nil
}

// Relink points a package at another Repository of its path owner. The path owner may always; anyone else must
// manage both the Repository it is linked to and the target.
func (s *PackageService) Relink(ctx context.Context, actorID, packageID, repoID int64) error {
	p, err := s.Package(ctx, packageID)
	if err != nil {
		return err
	}
	target, err := s.repos.GetByID(ctx, repoID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrRelinkTarget
	}
	if err != nil {
		return err
	}
	ownsTarget := p.OwnerUserID != nil && target.OrgID == 0 && target.OwnerID == *p.OwnerUserID ||
		p.OwnerOrgID != nil && target.OrgID == *p.OwnerOrgID
	if !ownsTarget {
		return ErrRelinkTarget
	}
	if !s.isPathOwner(ctx, p, actorID) {
		current, err := s.repos.GetByID(ctx, *p.RepoID)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if current == nil || !s.repos.CanManage(ctx, current, actorID) || !s.repos.CanManage(ctx, target, actorID) {
			return ErrPackageForbidden
		}
	}
	if err := s.packages.RelinkPackage(ctx, packageID, repoID); errors.Is(err, sql.ErrNoRows) {
		return ErrPackageNotFound
	} else if err != nil {
		return err
	}
	return nil
}

// LinkedPackageID is the id of the package of typ and name linked to repo, or ErrPackageNotFound. It looks the
// package up by linked Repository, not by path under the Repository's owner, which a transfer leaves behind.
func (s *PackageService) LinkedPackageID(ctx context.Context, repo *model.Repository, typ, name string) (int64, error) {
	list, err := s.packages.ListByRepo(ctx, repo.ID)
	if err != nil {
		return 0, err
	}
	for i := range list {
		if list[i].Type == typ && list[i].Name == name {
			return list[i].ID, nil
		}
	}
	return 0, ErrPackageNotFound
}

// InRepo returns the package of typ and name linked to repo, or ErrPackageNotFound, also when viewerID cannot read repo.
func (s *PackageService) InRepo(ctx context.Context, repo *model.Repository, viewerID *int64, typ, name string) (*model.PackageSummary, error) {
	return s.findInRepo(ctx, repo, viewerID, func(p *model.PackageSummary) bool { return p.Type == typ && p.Name == name })
}

// InRepoByID is InRepo for a package id, as a form carries it.
func (s *PackageService) InRepoByID(ctx context.Context, repo *model.Repository, viewerID *int64, id int64) (*model.PackageSummary, error) {
	return s.findInRepo(ctx, repo, viewerID, func(p *model.PackageSummary) bool { return p.ID == id })
}

func (s *PackageService) findInRepo(ctx context.Context, repo *model.Repository, viewerID *int64, match func(*model.PackageSummary) bool) (*model.PackageSummary, error) {
	list, err := s.ListByRepo(ctx, repo, viewerID)
	if err != nil {
		return nil, err
	}
	for i := range list {
		if match(&list[i]) {
			return &list[i], nil
		}
	}
	return nil, ErrPackageNotFound
}

// CountByRepo is how many packages are linked to repoID; the caller has checked that the viewer can read it.
func (s *PackageService) CountByRepo(ctx context.Context, repoID int64) (int, error) {
	return s.packages.CountByRepo(ctx, repoID)
}

// CanDelete reports whether userID may delete or re-link p: path ownership, or CanManage on the Repository it is
// linked to. It mirrors the rule Delete and Relink enforce, to decide which controls a page shows.
func (s *PackageService) CanDelete(ctx context.Context, p *model.Package, repo *model.Repository, userID int64) bool {
	return s.isPathOwner(ctx, p, userID) || repo != nil && s.repos.CanManage(ctx, repo, userID)
}

// Storage is what the path owner of p stores in packages and the quota it counts against; limit is 0 when there is none.
func (s *PackageService) Storage(ctx context.Context, p *model.Package) (used, limit int64, err error) {
	var o QuotaOwner
	if p.OwnerOrgID != nil {
		o.OrgID = *p.OwnerOrgID
	} else if p.OwnerUserID != nil {
		o.UserID = *p.OwnerUserID
	}
	if limit, err = s.limit(ctx, o); err != nil {
		return 0, 0, err
	}
	used, err = s.Usage(ctx, o)
	return used, limit, err
}

// UsageSummary reads "Packages 1.2 GiB of 10 GiB", or "Packages 1.2 GiB" when the owner has no package quota.
func (s *PackageService) UsageSummary(ctx context.Context, o QuotaOwner) (string, error) {
	limit, err := s.limit(ctx, o)
	if err != nil {
		return "", err
	}
	used, err := s.Usage(ctx, o)
	if err != nil {
		return "", err
	}
	if limit <= 0 {
		return "Packages " + FormatBytes(used), nil
	}
	return "Packages " + FormatBytes(used) + " of " + FormatBytes(limit), nil
}
