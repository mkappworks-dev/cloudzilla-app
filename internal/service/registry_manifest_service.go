package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const maxManifestBytes = 4 << 20

const (
	mediaTypeOCIManifest    = "application/vnd.oci.image.manifest.v1+json"
	mediaTypeOCIIndex       = "application/vnd.oci.image.index.v1+json"
	mediaTypeDockerManifest = "application/vnd.docker.distribution.manifest.v2+json"
	mediaTypeDockerList     = "application/vnd.docker.distribution.manifest.list.v2+json"
)

var (
	errManifestUnknown = regErr(http.StatusNotFound, "MANIFEST_UNKNOWN", "manifest unknown")
	errDenied          = regErr(http.StatusForbidden, "DENIED", "requested access to the resource is denied")

	registryTagRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
)

func isIndexType(mediaType string) bool {
	return mediaType == mediaTypeOCIIndex || mediaType == mediaTypeDockerList
}

func isManifestType(mediaType string) bool {
	return isIndexType(mediaType) || mediaType == mediaTypeOCIManifest || mediaType == mediaTypeDockerManifest
}

type descriptor struct {
	MediaType string   `json:"mediaType"`
	Digest    string   `json:"digest"`
	Size      int64    `json:"size"`
	URLs      []string `json:"urls"`
	Platform  *struct {
		OS           string `json:"os"`
		Architecture string `json:"architecture"`
		Variant      string `json:"variant"`
	} `json:"platform"`
}

type manifestDoc struct {
	SchemaVersion int          `json:"schemaVersion"`
	MediaType     string       `json:"mediaType"`
	ArtifactType  string       `json:"artifactType"`
	Config        *descriptor  `json:"config"`
	Layers        []descriptor `json:"layers"`
	Manifests     []descriptor `json:"manifests"`
	Subject       *descriptor  `json:"subject"`
}

func manifestInvalid(msg string) *RegistryError {
	return regErr(http.StatusBadRequest, "MANIFEST_INVALID", msg)
}

// parseManifest reads what a manifest of the given Content-Type names. Foreign layers, which carry URLs, are not
// stored by the registry and so need no link.
func parseManifest(mediaType string, content []byte) (store.ManifestRecord, *manifestDoc, error) {
	var doc manifestDoc
	if err := json.Unmarshal(content, &doc); err != nil {
		return store.ManifestRecord{}, nil, manifestInvalid("manifest is not valid JSON")
	}
	if doc.MediaType != "" && doc.MediaType != mediaType {
		return store.ManifestRecord{}, nil, manifestInvalid("manifest mediaType does not match its Content-Type")
	}
	rec := store.ManifestRecord{MediaType: mediaType, Content: content}
	seen := map[string]bool{}
	named := func(d descriptor, into *[]string) error {
		if !ValidBlobDigest(d.Digest) {
			return manifestInvalid("descriptor digest must be sha256:<64 hex digits>: " + d.Digest)
		}
		if !seen[d.Digest] && len(d.URLs) == 0 {
			seen[d.Digest] = true
			*into = append(*into, d.Digest)
		}
		return nil
	}
	if isIndexType(mediaType) {
		var platforms []string
		for _, d := range doc.Manifests {
			if err := named(d, &rec.Children); err != nil {
				return rec, nil, err
			}
			if p := platformOf(d); p != "" && !slices.Contains(platforms, p) {
				platforms = append(platforms, p)
			}
		}
		rec.Platforms = strings.Join(platforms, ",")
	} else {
		if doc.Config == nil {
			return rec, nil, manifestInvalid("manifest has no config")
		}
		if err := named(*doc.Config, &rec.Blobs); err != nil {
			return rec, nil, err
		}
		for _, d := range doc.Layers {
			if err := named(d, &rec.Blobs); err != nil {
				return rec, nil, err
			}
		}
	}
	if doc.Subject != nil {
		if !ValidBlobDigest(doc.Subject.Digest) {
			return rec, nil, manifestInvalid("subject digest must be sha256:<64 hex digits>")
		}
		rec.Subject = doc.Subject.Digest
	}
	rec.ArtifactType = doc.ArtifactType
	if rec.ArtifactType == "" && doc.Config != nil && doc.Subject != nil {
		rec.ArtifactType = doc.Config.MediaType
	}
	return rec, &doc, nil
}

// platformOf is "os/arch[/variant]" for a child that names one. Attestation children are "unknown/unknown" and are not
// a platform anyone pulls.
func platformOf(d descriptor) string {
	if d.Platform == nil || d.Platform.OS == "" || d.Platform.Architecture == "" || d.Platform.OS == "unknown" {
		return ""
	}
	p := d.Platform.OS + "/" + d.Platform.Architecture
	if d.Platform.Variant != "" {
		p += "/" + d.Platform.Variant
	}
	return p
}

func sha256Digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// PutManifest stores the manifest pushed to name at ref (a tag or a digest) and returns its digest and its subject
// digest, which is empty for a manifest without one.
func (s *RegistryService) PutManifest(ctx context.Context, userID int64, name, ref, mediaType string, body io.Reader) (digest, subject string, err error) {
	isDigest := strings.Contains(ref, ":")
	if isDigest && !ValidBlobDigest(ref) {
		return "", "", regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	if !isDigest && !registryTagRe.MatchString(ref) {
		return "", "", manifestInvalid("invalid tag")
	}
	if mediaType, _, _ = strings.Cut(mediaType, ";"); !isManifestType(strings.TrimSpace(mediaType)) {
		return "", "", manifestInvalid("unsupported manifest media type")
	}
	mediaType = strings.TrimSpace(mediaType)
	content, err := io.ReadAll(io.LimitReader(body, maxManifestBytes+1))
	if err != nil {
		return "", "", err
	}
	if len(content) > maxManifestBytes {
		return "", "", regErr(http.StatusRequestEntityTooLarge, "SIZE_INVALID", "manifest exceeds 4 MiB")
	}
	rec, doc, err := parseManifest(mediaType, content)
	if err != nil {
		return "", "", err
	}
	rec.Digest = sha256Digest(content)
	if isDigest && ref != rec.Digest {
		return "", "", &RegistryError{Status: http.StatusBadRequest, Code: "DIGEST_INVALID", Message: "the digest does not match the manifest", Detail: map[string]string{"computed": rec.Digest}}
	}

	t, err := s.writeTarget(ctx, userID, name, true)
	if err != nil {
		return "", "", err
	}
	if missing, err := s.unlinked(ctx, t.packageID, rec); err != nil {
		return "", "", err
	} else if missing != "" {
		return "", "", &RegistryError{Status: http.StatusBadRequest, Code: "MANIFEST_BLOB_UNKNOWN", Message: "a blob or manifest the manifest names is not in this package", Detail: map[string]string{"digest": missing}}
	}
	if t.pending {
		if err := s.linkPending(ctx, userID, t, name, doc); err != nil {
			return "", "", err
		}
	}
	if err := s.packages.CheckQuota(ctx, t.quotaOwner(), 0); err != nil {
		var q *PackageQuotaError
		if errors.As(err, &q) {
			return "", "", regErr(http.StatusForbidden, "DENIED", q.Error())
		}
		return "", "", err
	}
	tag := ""
	if !isDigest {
		tag = ref
	}
	if err := s.registry.PutManifest(ctx, t.packageID, rec, tag, userID); err != nil {
		return "", "", err
	}
	return rec.Digest, rec.Subject, nil
}

// unlinked returns the first blob or child the package does not hold, or "".
func (s *RegistryService) unlinked(ctx context.Context, packageID int64, rec store.ManifestRecord) (string, error) {
	blobs, err := s.registry.LinkedBlobs(ctx, packageID, rec.Blobs)
	if err != nil {
		return "", err
	}
	for _, d := range rec.Blobs {
		if !blobs[d] {
			return d, nil
		}
	}
	children, err := s.registry.ManifestDigests(ctx, packageID, rec.Children)
	if err != nil {
		return "", err
	}
	for _, d := range rec.Children {
		if !children[d] {
			return d, nil
		}
	}
	return "", nil
}

// Manifest is a stored manifest as a pull returns it.
type Manifest struct {
	ID        int64
	PackageID int64
	// Tag is the tag the pull named, empty for a pull by digest.
	Tag       string
	Digest    string
	MediaType string
	Content   []byte
	// Platforms is "os/arch[/variant]" for each platform an index names; empty for an image manifest.
	Platforms []string
}

// GetManifest returns the manifest at ref (a tag or a digest). A non-empty accept that excludes the stored media
// type answers MANIFEST_UNKNOWN, as a client that can't read the type would otherwise misparse the bytes.
func (s *RegistryService) GetManifest(ctx context.Context, userID int64, name, ref string, accept []string) (*Manifest, error) {
	t, err := s.readTarget(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	var m *store.StoredManifest
	byTag := !strings.Contains(ref, ":")
	if !byTag {
		if !ValidBlobDigest(ref) {
			return nil, regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
		}
		m, err = s.registry.ManifestByDigest(ctx, t.packageID, ref)
	} else {
		m, err = s.registry.ManifestByTag(ctx, t.packageID, ref)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errManifestUnknown
	}
	if err != nil {
		return nil, err
	}
	if !acceptsMediaType(accept, m.MediaType) {
		return nil, errManifestUnknown
	}
	var platforms []string
	if m.Platforms != "" {
		platforms = strings.Split(m.Platforms, ",")
	}
	out := &Manifest{ID: m.ID, PackageID: t.packageID, Digest: m.Digest, MediaType: m.MediaType, Content: m.Content, Platforms: platforms}
	if byTag {
		out.Tag = ref
	}
	return out, nil
}

func acceptsMediaType(accept []string, mediaType string) bool {
	any := false
	for _, header := range accept {
		for _, part := range strings.Split(header, ",") {
			t, _, _ := strings.Cut(part, ";")
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			any = true
			if t == mediaType || t == "*/*" || t == "application/*" {
				return true
			}
		}
	}
	return !any
}

// DeleteManifest deletes the manifest at digest with its tags, or only the tag when ref names one. An index child
// cannot be deleted while an index names it.
func (s *RegistryService) DeleteManifest(ctx context.Context, userID int64, name, ref string) error {
	isDigest := strings.Contains(ref, ":")
	if isDigest && !ValidBlobDigest(ref) {
		return regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	t, err := s.writeTarget(ctx, userID, name, false)
	if err != nil {
		return err
	}
	var found bool
	if isDigest {
		var child bool
		if child, err = s.registry.IsManifestChild(ctx, t.packageID, ref); err != nil {
			return err
		}
		if child {
			return regErr(http.StatusConflict, "DENIED", "the manifest is referenced by an index; delete the index first")
		}
		found, err = s.registry.DeleteManifest(ctx, t.packageID, ref)
	} else {
		found, err = s.registry.DeleteTag(ctx, t.packageID, ref)
	}
	if err != nil {
		return err
	}
	if !found {
		return errManifestUnknown
	}
	return nil
}

// DeleteBlob unlinks the blob from the package unless a manifest of it still names the blob. The object stays for
// the sweep, which deletes it once no package links it.
func (s *RegistryService) DeleteBlob(ctx context.Context, userID int64, name, digest string) error {
	if !ValidBlobDigest(digest) {
		return regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	t, err := s.writeTarget(ctx, userID, name, false)
	if err != nil {
		return err
	}
	held, err := s.registry.BlobReferenced(ctx, t.packageID, digest)
	if err != nil {
		return err
	}
	if held {
		return regErr(http.StatusConflict, "DENIED", "the blob is referenced by a manifest; delete the manifest first")
	}
	found, err := s.registry.UnlinkBlob(ctx, t.packageID, digest)
	if err != nil {
		return err
	}
	if !found {
		return errBlobUnknown
	}
	return nil
}

// Referrers returns the package's manifests whose subject is subject. An unknown subject is an empty list, never an
// error: the subject may be pushed later, or live in another registry.
func (s *RegistryService) Referrers(ctx context.Context, userID int64, name, subject, artifactType string) ([]store.Referrer, error) {
	if !ValidBlobDigest(subject) {
		return nil, regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	t, err := s.readTarget(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	return s.registry.Referrers(ctx, t.packageID, subject, artifactType)
}

// ListTags returns up to limit tags after last, in byte order, and whether more follow. A negative limit lists all.
func (s *RegistryService) ListTags(ctx context.Context, userID int64, name, last string, limit int) ([]string, bool, error) {
	t, err := s.readTarget(ctx, userID, name)
	if err != nil {
		return nil, false, err
	}
	if limit == 0 {
		return []string{}, false, nil
	}
	fetch := limit
	if limit > 0 {
		fetch = limit + 1
	}
	tags, err := s.registry.TagNames(ctx, t.packageID, last, fetch)
	if err != nil {
		return nil, false, err
	}
	if limit > 0 && len(tags) > limit {
		return tags[:limit], true, nil
	}
	return tags, false, nil
}
