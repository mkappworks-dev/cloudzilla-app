package service

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5/plumbing/protocol/packp"

	"github.com/mkappworks-dev/cloudzilla-app/internal/concurrency"
	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/gittransport"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

// ErrQuotaReached matches every *QuotaError.
var ErrQuotaReached = errors.New("quota reached")

// QuotaError is what a refusal for quota carries. Its message is shown to the user as is.
type QuotaError struct {
	Storage bool // false: the repository count
	Used    int64
	Limit   int64
}

func (e *QuotaError) Error() string {
	if e.Storage {
		return fmt.Sprintf("storage quota reached (%s of %s)", FormatBytes(e.Used), FormatBytes(e.Limit))
	}
	return fmt.Sprintf("repository quota reached (%d of %d)", e.Used, e.Limit)
}

func (e *QuotaError) Is(target error) bool { return target == ErrQuotaReached }

// QuotaOwner is the namespace a quota is counted in: an org when OrgID is set, else a user.
type QuotaOwner struct{ UserID, OrgID int64 }

func RepoQuotaOwner(r *model.Repository) QuotaOwner {
	return QuotaOwner{UserID: r.OwnerID, OrgID: r.OrgID}
}

func targetQuotaOwner(t RepoTarget) QuotaOwner { return QuotaOwner{UserID: t.OwnerID, OrgID: t.OrgID} }

// QuotaUsage is an owner's use of the limits that apply to it; a zero limit is not set.
type QuotaUsage struct {
	Repos  int
	Bytes  int64
	Limits config.QuotaLimits
}

// Summary is one line naming only the limits that are set, "" when none is.
func (u QuotaUsage) Summary() string {
	var parts []string
	if u.Limits.Repos > 0 {
		parts = append(parts, fmt.Sprintf("Repositories %d of %d", u.Repos, u.Limits.Repos))
	}
	if u.Limits.StorageBytes > 0 {
		parts = append(parts, fmt.Sprintf("Storage %s of %s", FormatBytes(u.Bytes), FormatBytes(u.Limits.StorageBytes)))
	}
	return strings.Join(parts, " · ")
}

// FormatBytes prints n in binary units with one decimal, none when it is whole.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	v := fmt.Sprintf("%.1f", float64(n)/float64(div))
	return strings.TrimSuffix(v, ".0") + " " + string("KMGTPE"[exp]) + "iB"
}

// QuotaService enforces per-owner repository and storage quotas and keeps
// repositories.size_bytes, the usage they are checked against. Every method is
// a no-op on a nil receiver, so tests that don't exercise quotas leave it unset.
type QuotaService struct {
	repos *store.RepoStore
	users *store.UserStore
	cfg   config.QuotaConfig
	root  string

	mu     sync.Mutex
	sizing map[int64]*sizeJob
	wg     sync.WaitGroup
}

type sizeJob struct {
	owner, name string
	again       bool
}

func NewQuotaService(repos *store.RepoStore, users *store.UserStore, cfg config.QuotaConfig, reposRoot string) *QuotaService {
	return &QuotaService{repos: repos, users: users, cfg: cfg, root: reposRoot, sizing: map[int64]*sizeJob{}}
}

// limits are the zero value, meaning unlimited, for a superadmin's personal account.
func (q *QuotaService) limits(ctx context.Context, o QuotaOwner) (config.QuotaLimits, error) {
	if o.OrgID != 0 {
		return q.cfg.Org, nil
	}
	if q.cfg.User == (config.QuotaLimits{}) {
		return q.cfg.User, nil
	}
	u, err := q.users.GetByID(ctx, o.UserID)
	if err != nil {
		return config.QuotaLimits{}, fmt.Errorf("load quota owner: %w", err)
	}
	if u.IsSuperadmin {
		return config.QuotaLimits{}, nil
	}
	return q.cfg.User, nil
}

// Usage reports o's use of its limits.
func (q *QuotaService) Usage(ctx context.Context, o QuotaOwner) (QuotaUsage, error) {
	if q == nil {
		return QuotaUsage{}, nil
	}
	limits, err := q.limits(ctx, o)
	if err != nil {
		return QuotaUsage{}, err
	}
	repos, bytes, err := q.repos.OwnerUsage(ctx, o.UserID, o.OrgID)
	return QuotaUsage{Repos: repos, Bytes: bytes, Limits: limits}, err
}

// CheckNewRepo refuses a repository that would take o past its count quota.
// It checks and the caller creates afterwards, so creates racing each other
// can overshoot by as many as run at once.
func (q *QuotaService) CheckNewRepo(ctx context.Context, o QuotaOwner) error {
	if q == nil {
		return nil
	}
	limits, err := q.limits(ctx, o)
	if err != nil || limits.Repos <= 0 {
		return err
	}
	repos, _, err := q.repos.OwnerUsage(ctx, o.UserID, o.OrgID)
	if err != nil {
		return err
	}
	if repos >= limits.Repos {
		return &QuotaError{Used: int64(repos), Limit: int64(limits.Repos)}
	}
	return nil
}

// usedStorage is o's bytes in use and its limit, with a limit of 0 when none applies.
func (q *QuotaService) usedStorage(ctx context.Context, o QuotaOwner) (used, limit int64, err error) {
	limits, err := q.limits(ctx, o)
	if err != nil || limits.StorageBytes <= 0 {
		return 0, 0, err
	}
	_, used, err = q.repos.OwnerUsage(ctx, o.UserID, o.OrgID)
	return used, limits.StorageBytes, err
}

// CheckStorage refuses a web write to a repo whose owner is at or over its storage quota.
func (q *QuotaService) CheckStorage(ctx context.Context, repo *model.Repository) error {
	if q == nil {
		return nil
	}
	used, limit, err := q.usedStorage(ctx, RepoQuotaOwner(repo))
	if err != nil {
		return err
	}
	if limit > 0 && used >= limit {
		return &QuotaError{Storage: true, Used: used, Limit: limit}
	}
	return nil
}

// CapPush stops limiter, which has read cmds from a push to repo, at the space
// left in the owner's storage quota. A push that only deletes refs is left
// alone. It returns the refusal to send if limiter.Exceeded afterwards, or nil
// when no quota cap applies or git.max_pack_bytes is the tighter one.
func (q *QuotaService) CapPush(ctx context.Context, repo *model.Repository, cmds []*packp.Command, limiter *gittransport.LimitedReadCloser) (*QuotaError, error) {
	if q == nil || onlyDeletes(cmds) {
		return nil, nil
	}
	used, limit, err := q.usedStorage(ctx, RepoQuotaOwner(repo))
	if err != nil || limit <= 0 {
		return nil, err
	}
	if !limiter.LimitTo(limit - used) {
		return nil, nil
	}
	return &QuotaError{Storage: true, Used: used, Limit: limit}, nil
}

func onlyDeletes(cmds []*packp.Command) bool {
	for _, c := range cmds {
		if c.Action() != packp.Delete {
			return false
		}
	}
	return true
}

// Recompute re-measures repo in the background after a write. Calls for a repo
// already being measured collapse into one more pass.
func (q *QuotaService) Recompute(repo *model.Repository) {
	if q == nil || repo == nil {
		return
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	if job, running := q.sizing[repo.ID]; running {
		job.owner, job.name, job.again = repo.OwnerName, repo.Name, true
		return
	}
	job := &sizeJob{owner: repo.OwnerName, name: repo.Name}
	q.sizing[repo.ID] = job
	q.wg.Add(1)
	concurrency.Go("quota.recompute", func() {
		defer q.wg.Done()
		q.measureUntilIdle(repo.ID, job)
	})
}

func (q *QuotaService) measureUntilIdle(id int64, job *sizeJob) {
	for {
		q.mu.Lock()
		owner, name := job.owner, job.name
		job.again = false
		q.mu.Unlock()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		if err := q.measure(ctx, id, owner, name); err != nil {
			slog.Error("quota: measure repo size", "repo_id", id, "owner", owner, "name", name, "error", err)
		}
		cancel()

		q.mu.Lock()
		if !job.again {
			delete(q.sizing, id)
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()
	}
}

func (q *QuotaService) measure(ctx context.Context, id int64, owner, name string) error {
	gitDir, wikiDir := repoDirs(q.root, owner, name)
	size, err := DirSize(gitDir, wikiDir)
	if err != nil {
		return err
	}
	return q.repos.SetSize(ctx, id, size)
}

// Wait blocks until every pending Recompute has finished.
func (q *QuotaService) Wait() {
	if q != nil {
		q.wg.Wait()
	}
}

// Backfill measures every repo that has no size yet. It is meant for startup.
func (q *QuotaService) Backfill(ctx context.Context) {
	if q == nil {
		return
	}
	pending, err := q.repos.ListUnmeasured(ctx)
	if err != nil {
		slog.Error("quota: list unmeasured repos", "error", err)
		return
	}
	for _, r := range pending {
		if ctx.Err() != nil {
			return
		}
		if err := q.measure(ctx, r.ID, r.OwnerName, r.Name); err != nil {
			slog.Error("quota: backfill repo size", "repo_id", r.ID, "owner", r.OwnerName, "name", r.Name, "error", err)
		}
	}
	if len(pending) > 0 {
		slog.Info("quota: measured repository sizes", "repos", len(pending))
	}
}

// DirSize totals the regular files under dirs. A dir that doesn't exist counts
// 0, and so does a file that vanishes mid-walk, as a push or gc may remove one.
func DirSize(dirs ...string) (int64, error) {
	var total int64
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			info, err := d.Info()
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			if err != nil {
				return err
			}
			total += info.Size()
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("measure %s: %w", dir, err)
		}
	}
	return total, nil
}
