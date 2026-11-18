package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/storage"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// RegistryError is a failure the wire protocol answers with an OCI error body.
type RegistryError struct {
	Status  int
	Code    string
	Message string
	Detail  any
	// Headers are set on the response, e.g. Range on a 416.
	Headers map[string]string
}

func (e *RegistryError) Error() string { return e.Code + ": " + e.Message }

func regErr(status int, code, message string) *RegistryError {
	return &RegistryError{Status: status, Code: code, Message: message}
}

var (
	errNameUnknown   = regErr(http.StatusNotFound, "NAME_UNKNOWN", "repository name not known to registry")
	errBlobUnknown   = regErr(http.StatusNotFound, "BLOB_UNKNOWN", "blob unknown to registry")
	errUploadUnknown = regErr(http.StatusNotFound, "BLOB_UPLOAD_UNKNOWN", "blob upload unknown to registry")
	errNoStorage     = regErr(http.StatusInternalServerError, "UNSUPPORTED", "registry storage is not configured")
)

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("session id: %w", err)
	}
	b[6], b[8] = b[6]&0x0f|0x40, b[8]&0x3f|0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:], nil
}

var blobDigestRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// ValidBlobDigest reports whether digest is a lowercase sha256 digest, the only algorithm the registry stores.
func ValidBlobDigest(digest string) bool { return blobDigestRe.MatchString(digest) }

// blobKey is hex only so it passes storage.ValidKey whatever the digest says.
func blobKey(digest string) string { return "registry/" + digest[len("sha256:"):] }

// WithBackend gives the service the object store blobs are put in.
func (s *RegistryService) WithBackend(b storage.Backend) *RegistryService {
	s.backend = b
	return s
}

// WithSpoolDir names the directory uploads spool to.
func (s *RegistryService) WithSpoolDir(dir string) *RegistryService {
	s.spoolDir = dir
	return s
}

// ResetSpool empties dir and forgets every open session: a restart loses in-flight uploads, so a session row
// whose file is gone must not count against its user's caps.
func (s *RegistryService) ResetSpool(ctx context.Context, dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("empty registry spool: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create registry spool: %w", err)
	}
	s.spoolDir = dir
	return s.registry.DeleteAllSessions(ctx)
}

// UploadState is what the Location, Range and Docker-Upload-UUID headers of an upload answer are made of.
type UploadState struct {
	ID    string
	Bytes int64
}

// uploadTarget is the package an upload or pull acts on.
type uploadTarget struct {
	packageID int64
	owner     store.PathOwner
	pending   bool
}

func (t uploadTarget) quotaOwner() QuotaOwner {
	return QuotaOwner{UserID: t.owner.UserID, OrgID: t.owner.OrgID}
}

func (s *RegistryService) resolveOwner(ctx context.Context, name string) (store.PathOwner, string, error) {
	segment, image, ok := strings.Cut(name, "/")
	if !ok || !ValidRegistryName(name) {
		return store.PathOwner{}, "", errNameUnknown
	}
	owner, err := s.packages.packages.ResolveOwner(ctx, segment)
	if errors.Is(err, sql.ErrNoRows) {
		return store.PathOwner{}, "", errNameUnknown
	}
	return owner, image, err
}

// readTarget finds the package a pull acts on. A private, soft-deleted, unknown or someone else's pending name all
// answer NAME_UNKNOWN. Pending packages are readable by their creator only, so a pusher can HEAD what they sent.
func (s *RegistryService) readTarget(ctx context.Context, userID int64, name string) (uploadTarget, error) {
	owner, image, err := s.resolveOwner(ctx, name)
	if err != nil {
		return uploadTarget{}, err
	}
	pkgID, repoID, err := s.registry.LinkedPackage(ctx, owner, image)
	switch {
	case err == nil:
		repo, err := s.repos.GetByID(ctx, repoID)
		if errors.Is(err, sql.ErrNoRows) {
			return uploadTarget{}, errNameUnknown
		}
		if err != nil {
			return uploadTarget{}, err
		}
		var viewer *int64
		if userID != 0 {
			viewer = &userID
		}
		if !s.repos.CanRead(ctx, repo, viewer) {
			return uploadTarget{}, errNameUnknown
		}
		return uploadTarget{packageID: pkgID, owner: owner}, nil
	case !errors.Is(err, sql.ErrNoRows):
		return uploadTarget{}, err
	}
	if userID != 0 {
		id, err := s.registry.PendingPackage(ctx, owner, image, userID, s.now())
		if err == nil {
			return uploadTarget{packageID: id, owner: owner, pending: true}, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return uploadTarget{}, err
		}
	}
	return uploadTarget{}, errNameUnknown
}

// writeTarget is readTarget for a caller who pushes: a reader of a linked package is DENIED, and with create set a
// new name gets a pending package for the caller.
func (s *RegistryService) writeTarget(ctx context.Context, userID int64, name string, create bool) (uploadTarget, error) {
	t, err := s.readTarget(ctx, userID, name)
	switch {
	case err == nil:
		if t.pending {
			return t, nil
		}
		repoID, err := s.linkedRepo(ctx, t)
		if err != nil {
			return uploadTarget{}, err
		}
		repo, err := s.repos.GetByID(ctx, repoID)
		if err != nil {
			return uploadTarget{}, err
		}
		if !s.repos.CanWrite(ctx, repo, userID) {
			return uploadTarget{}, regErr(http.StatusForbidden, "DENIED", "requested access to the resource is denied")
		}
		return t, nil
	case !errors.Is(err, errNameUnknown) || !create || userID == 0:
		return uploadTarget{}, err
	}
	return s.startPending(ctx, userID, name)
}

func (s *RegistryService) linkedRepo(ctx context.Context, t uploadTarget) (int64, error) {
	return s.registry.PackageRepo(ctx, t.packageID)
}

// startPending creates the caller's pending package for a name no Repository owns yet.
func (s *RegistryService) startPending(ctx context.Context, userID int64, name string) (uploadTarget, error) {
	owner, image, err := s.resolveOwner(ctx, name)
	if err != nil {
		return uploadTarget{}, err
	}
	// Linked is excluded by the caller, so only a writer anywhere in the namespace may claim a new name.
	if may, err := s.writesAnyOf(ctx, owner, userID); err != nil {
		return uploadTarget{}, err
	} else if !may {
		return uploadTarget{}, regErr(http.StatusForbidden, "DENIED", "requested access to the resource is denied")
	}
	now := s.now()
	stale, err := s.registry.DeleteExpiredPending(ctx, owner, image, userID, now)
	if err != nil {
		return uploadTarget{}, err
	}
	removeSpool(stale)
	held, err := s.registry.CountPending(ctx, userID, now)
	if err != nil {
		return uploadTarget{}, err
	}
	if held >= s.cfg.MaxPendingPerUser {
		return uploadTarget{}, regErr(http.StatusTooManyRequests, "TOOMANYREQUESTS", "too many unpublished packages; push a manifest or wait for them to expire")
	}
	id, err := s.registry.CreatePending(ctx, owner, image, userID, now.Add(s.cfg.UploadTTL))
	if errors.Is(err, store.ErrPendingExists) {
		id, err = s.registry.PendingPackage(ctx, owner, image, userID, now)
	}
	if err != nil {
		return uploadTarget{}, err
	}
	return uploadTarget{packageID: id, owner: owner, pending: true}, nil
}

func removeSpool(paths []string) {
	for _, p := range paths {
		_ = os.Remove(p)
	}
}

// StartUpload opens an upload session for userID on name.
func (s *RegistryService) StartUpload(ctx context.Context, userID int64, name string) (*UploadState, error) {
	if s.backend == nil || s.spoolDir == "" {
		return nil, errNoStorage
	}
	t, err := s.writeTarget(ctx, userID, name, true)
	if err != nil {
		return nil, err
	}
	now := s.now()
	open, _, err := s.registry.OpenSessions(ctx, userID, now.Add(-s.cfg.UploadTTL))
	if err != nil {
		return nil, err
	}
	if open >= s.cfg.MaxSessionsPerUser {
		return nil, regErr(http.StatusTooManyRequests, "TOOMANYREQUESTS", "too many uploads in progress")
	}
	id, err := newUUID()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(s.spoolDir, id)
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return nil, fmt.Errorf("create spool file: %w", err)
	}
	if err := s.registry.CreateSession(ctx, store.UploadSession{ID: id, PackageID: t.packageID, UserID: userID, TempPath: path, CreatedAt: now}); err != nil {
		_ = os.Remove(path)
		return nil, err
	}
	return &UploadState{ID: id}, nil
}

// session loads the open session id for userID on name. Another user's session, another package's, an expired one
// and one whose spool file is gone all answer BLOB_UPLOAD_UNKNOWN.
func (s *RegistryService) session(ctx context.Context, userID int64, name, id string) (*store.UploadSession, uploadTarget, error) {
	if !uuidRe.MatchString(id) {
		return nil, uploadTarget{}, errUploadUnknown
	}
	t, err := s.writeTarget(ctx, userID, name, false)
	if errors.Is(err, errNameUnknown) {
		return nil, uploadTarget{}, errUploadUnknown
	}
	if err != nil {
		return nil, uploadTarget{}, err
	}
	sess, err := s.registry.Session(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, uploadTarget{}, errUploadUnknown
	}
	if err != nil {
		return nil, uploadTarget{}, err
	}
	now := s.now()
	if sess.UserID != userID || sess.PackageID != t.packageID || !sess.CreatedAt.Add(s.cfg.UploadTTL).After(now) ||
		(sess.PendingExpiresAt != nil && !sess.PendingExpiresAt.After(now)) {
		return nil, uploadTarget{}, errUploadUnknown
	}
	return sess, t, nil
}

// UploadStatus reports how many bytes the session holds.
func (s *RegistryService) UploadStatus(ctx context.Context, userID int64, name, id string) (*UploadState, error) {
	sess, _, err := s.session(ctx, userID, name, id)
	if err != nil {
		return nil, err
	}
	return &UploadState{ID: sess.ID, Bytes: sess.Bytes}, nil
}

// CancelUpload drops the session and its spool file.
func (s *RegistryService) CancelUpload(ctx context.Context, userID int64, name, id string) error {
	sess, _, err := s.session(ctx, userID, name, id)
	if err != nil {
		return err
	}
	return s.dropSession(ctx, sess)
}

func (s *RegistryService) dropSession(ctx context.Context, sess *store.UploadSession) error {
	_ = os.Remove(sess.TempPath)
	return s.registry.DeleteSession(ctx, sess.ID)
}

// ContentRange is an optional "start-end" a client sent with a chunk.
type ContentRange struct{ Start, End int64 }

// AppendUpload appends body to the session. A range, when sent, must start where the session ends (else 416 with the
// Range of what it holds) and span exactly the bytes sent.
func (s *RegistryService) AppendUpload(ctx context.Context, userID int64, name, id string, body io.Reader, rng *ContentRange) (*UploadState, error) {
	sess, t, err := s.session(ctx, userID, name, id)
	if err != nil {
		return nil, err
	}
	n, err := s.appendSpool(ctx, sess, t, body, rng)
	if err != nil {
		return nil, err
	}
	return &UploadState{ID: sess.ID, Bytes: sess.Bytes + n}, nil
}

func rangeNotSatisfiable(have int64) *RegistryError {
	e := regErr(http.StatusRequestedRangeNotSatisfiable, "BLOB_UPLOAD_INVALID", "chunk is out of order")
	e.Headers = map[string]string{"Range": uploadRange(have)}
	return e
}

// UploadRange formats the Range header: the inclusive span of bytes held, "0-0" when empty.
func UploadRange(have int64) string { return uploadRange(have) }

func uploadRange(have int64) string {
	if have == 0 {
		return "0-0"
	}
	return "0-" + strconv.FormatInt(have-1, 10)
}

// appendSpool copies body onto the spool file and records the new size. It writes at most what every cap allows
// and, when the body is longer, truncates back and refuses, so a refused chunk leaves the session as it was.
func (s *RegistryService) appendSpool(ctx context.Context, sess *store.UploadSession, t uploadTarget, body io.Reader, rng *ContentRange) (int64, error) {
	if rng != nil && rng.Start != sess.Bytes {
		return 0, rangeNotSatisfiable(sess.Bytes)
	}
	allowed, refusal, err := s.allowance(ctx, sess, t)
	if err != nil {
		return 0, err
	}
	f, err := os.OpenFile(sess.TempPath, os.O_WRONLY, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		_ = s.registry.DeleteSession(ctx, sess.ID)
		return 0, errUploadUnknown
	}
	if err != nil {
		return 0, fmt.Errorf("open spool file: %w", err)
	}
	defer func() { _ = f.Close() }()
	rollback := func() { _ = f.Truncate(sess.Bytes) }
	if err := f.Truncate(sess.Bytes); err != nil {
		return 0, fmt.Errorf("truncate spool file: %w", err)
	}
	if _, err := f.Seek(sess.Bytes, io.SeekStart); err != nil {
		return 0, fmt.Errorf("seek spool file: %w", err)
	}
	n, err := io.Copy(f, io.LimitReader(body, allowed+1))
	if err != nil {
		rollback()
		return 0, fmt.Errorf("spool upload: %w", err)
	}
	if n > allowed {
		rollback()
		return 0, refusal
	}
	if rng != nil && rng.End-rng.Start+1 != n {
		rollback()
		return 0, regErr(http.StatusBadRequest, "SIZE_INVALID", "the chunk does not match its Content-Range")
	}
	if n > 0 {
		if err := s.registry.SetSessionBytes(ctx, sess.ID, sess.Bytes+n); err != nil {
			rollback()
			return 0, err
		}
	}
	return n, nil
}

// allowance is how many more bytes the session may take and the error to answer when a body goes past it. The
// tightest of the blob cap, the user's in-flight cap and the owner's quota wins.
func (s *RegistryService) allowance(ctx context.Context, sess *store.UploadSession, t uploadTarget) (int64, *RegistryError, error) {
	allowed := s.cfg.MaxBlobBytes - sess.Bytes
	refusal := regErr(http.StatusRequestEntityTooLarge, "SIZE_INVALID", "blob exceeds the maximum size")

	_, held, err := s.registry.OpenSessions(ctx, sess.UserID, s.now().Add(-s.cfg.UploadTTL))
	if err != nil {
		return 0, nil, err
	}
	if room := s.cfg.MaxInflightBytesPerUser - held; room < allowed {
		allowed, refusal = room, regErr(http.StatusTooManyRequests, "TOOMANYREQUESTS", "too many bytes in flight for this user")
	}

	room, err := s.packages.Headroom(ctx, t.quotaOwner())
	if err != nil {
		return 0, nil, err
	}
	if room >= 0 && room < allowed {
		used, uerr := s.packages.Usage(ctx, t.quotaOwner())
		if uerr != nil {
			return 0, nil, uerr
		}
		allowed = room
		refusal = regErr(http.StatusForbidden, "DENIED", (&PackageQuotaError{Used: used, Limit: used + room}).Error())
	}
	return max(allowed, 0), refusal, nil
}

// CompleteUpload ends the session: the optional final chunk in body is appended, the sha256 and size are checked,
// and the blob is stored and linked to the package. It returns the digest.
func (s *RegistryService) CompleteUpload(ctx context.Context, userID int64, name, id, digest string, body io.Reader, rng *ContentRange) (string, error) {
	if !ValidBlobDigest(digest) {
		return "", regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	sess, t, err := s.session(ctx, userID, name, id)
	if err != nil {
		return "", err
	}
	if body != nil {
		if _, err := s.appendSpool(ctx, sess, t, body, rng); err != nil {
			return "", err
		}
	}
	// Hashed here rather than while spooling: chunks arrive in separate requests and a hash state would have to be
	// persisted between them. The cost is one extra local read per commit.
	sum, size, err := hashFile(sess.TempPath)
	if errors.Is(err, os.ErrNotExist) {
		_ = s.registry.DeleteSession(ctx, sess.ID)
		return "", errUploadUnknown
	}
	if err != nil {
		return "", err
	}
	if got := "sha256:" + sum; got != digest {
		_ = s.dropSession(ctx, sess)
		return "", &RegistryError{Status: http.StatusBadRequest, Code: "DIGEST_INVALID", Message: "the digest does not match the uploaded content", Detail: map[string]string{"computed": got}}
	}
	// Concurrent sessions may each have passed the PATCH check.
	if err := s.packages.CheckQuota(ctx, t.quotaOwner(), 0); err != nil {
		var q *PackageQuotaError
		if errors.As(err, &q) {
			_ = s.dropSession(ctx, sess)
			return "", regErr(http.StatusForbidden, "DENIED", q.Error())
		}
		return "", err
	}
	if err := s.storeBlob(ctx, t.packageID, digest, sess.TempPath, size); err != nil {
		return "", err
	}
	return digest, s.dropSession(ctx, sess)
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, fmt.Errorf("hash spool file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// storeBlob links the blob to the package, putting the object first when no row says it is stored. The link is
// written under FOR SHARE on the blob row so the sweep cannot delete the object between the check and the link; a
// row that vanished since is the sweep having won, and the object is put again.
func (s *RegistryService) storeBlob(ctx context.Context, packageID int64, digest, path string, size int64) error {
	linked, err := s.registry.LinkExistingBlob(ctx, packageID, digest)
	if err != nil || linked {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open spool file: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := s.backend.Put(ctx, blobKey(digest), f, size); err != nil {
		return fmt.Errorf("store blob: %w", err)
	}
	return s.registry.InsertAndLinkBlob(ctx, packageID, digest, size)
}

// StatBlob is the size of the blob name links at digest.
func (s *RegistryService) StatBlob(ctx context.Context, userID int64, name, digest string) (int64, error) {
	if !ValidBlobDigest(digest) {
		return 0, regErr(http.StatusBadRequest, "DIGEST_INVALID", "digest must be sha256:<64 hex digits>")
	}
	t, err := s.readTarget(ctx, userID, name)
	if err != nil {
		return 0, err
	}
	size, err := s.registry.BlobSize(ctx, t.packageID, digest)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errBlobUnknown
	}
	return size, err
}

// OpenBlob streams the blob; the caller closes it.
func (s *RegistryService) OpenBlob(ctx context.Context, userID int64, name, digest string) (io.ReadCloser, int64, error) {
	size, err := s.StatBlob(ctx, userID, name, digest)
	if err != nil {
		return nil, 0, err
	}
	rc, err := s.backend.Get(ctx, blobKey(digest))
	if errors.Is(err, storage.ErrNotFound) {
		return nil, 0, errBlobUnknown
	}
	if err != nil {
		return nil, 0, fmt.Errorf("open blob: %w", err)
	}
	return rc, size, nil
}

// MountBlob links the blob at digest from another package into name, when userID may read that package. It
// reports false, never an error, for everything else, so the client falls back to a normal upload.
func (s *RegistryService) MountBlob(ctx context.Context, userID int64, name, from, digest string) (bool, error) {
	if !ValidBlobDigest(digest) || from == name {
		return false, nil
	}
	src, err := s.readTarget(ctx, userID, from)
	if errors.Is(err, errNameUnknown) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	dst, err := s.writeTarget(ctx, userID, name, true)
	if err != nil {
		return false, err
	}
	return s.registry.CopyBlobLink(ctx, src.packageID, dst.packageID, digest)
}
