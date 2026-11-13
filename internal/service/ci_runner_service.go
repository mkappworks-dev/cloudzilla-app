package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
)

const (
	registrationTokenPrefix = "czreg_"
	runnerTokenPrefix       = "czrun_"
	// JobTokenPrefix marks the per-job token; the git HTTP path tells it apart from a PAT by this.
	JobTokenPrefix = "czj_"

	registrationTokenTTL = 7 * 24 * time.Hour
	lostJobSweepEvery    = 15 * time.Second
	// contactTouchEvery caps how often authenticating rewrites last_contact_at: a waiting claim authenticates every poll.
	contactTouchEvery = 5 * time.Second
)

var (
	ErrRunnerUnauthorized = errors.New("runner token is invalid or revoked")
	ErrRegistrationToken  = errors.New("registration token is invalid, used or expired")
	ErrJobNotHeld         = errors.New("job is not held by this runner")
	ErrInvalidRunnerLabel = errors.New("labels must be 1-64 characters of letters, digits, '.', '_' or '-', and at least one is required")
)

var runnerLabelRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// JobToken is what a verified per-job token grants: read access to one repo for one running Job.
type JobToken struct {
	JobID  int64
	RunID  int64
	RepoID int64
	// CloneRepoID is the one repository the token may fetch: the Run's, or for a fork pull request's Run the fork
	// that holds the commit.
	CloneRepoID int64
	// NoSecrets is set for a Run that never received secrets, so there is nothing to mask.
	NoSecrets bool
}

// CIRunnerService owns runners, their tokens and the per-job token. Scheduling stays in WorkflowRunService.
type CIRunnerService struct {
	runners *store.CIRunnerStore
	runs    *WorkflowRunService
	repos   *store.RepoStore
	baseURL string
	secrets *CISecretService
}

// WithSecrets lets a claim carry the repo's secrets to a Run that may have them.
func (s *CIRunnerService) WithSecrets(src *CISecretService) *CIRunnerService {
	s.secrets = src
	return s
}

func NewCIRunnerService(runners *store.CIRunnerStore, runs *WorkflowRunService, repos *store.RepoStore, baseURL string) *CIRunnerService {
	return &CIRunnerService{runners: runners, runs: runs, repos: repos, baseURL: strings.TrimRight(baseURL, "/")}
}

func newSecret(prefix string) (raw, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", fmt.Errorf("generate %s token: %w", prefix, err)
	}
	raw = prefix + hex.EncodeToString(b)
	return raw, hashSecret(raw), nil
}

func hashSecret(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// CreateRegistrationToken makes a one-time token for a runner of repo's scope, or of the Instance when repo is nil.
// The caller has already checked that userID may manage that scope. The raw token is returned once and never stored.
func (s *CIRunnerService) CreateRegistrationToken(ctx context.Context, repo *model.Repository, userID int64) (string, error) {
	raw, hash, err := newSecret(registrationTokenPrefix)
	if err != nil {
		return "", err
	}
	t := &model.CIRegistrationToken{Scope: model.RunnerScopeInstance, CreatedBy: &userID, ExpiresAt: time.Now().Add(registrationTokenTTL)}
	if repo != nil {
		t.Scope, t.RepoID = model.RunnerScopeRepository, &repo.ID
	}
	if err := s.runners.InsertRegistrationToken(ctx, t, hash); err != nil {
		return "", err
	}
	return raw, nil
}

// Register trades a one-time registration token for a runner and its long-lived token.
func (s *CIRunnerService) Register(ctx context.Context, regToken, name string, labels []string, version string) (*model.CIRunner, string, error) {
	labels = slices.Compact(slices.Sorted(slices.Values(labels)))
	if len(labels) == 0 {
		return nil, "", ErrInvalidRunnerLabel
	}
	for _, l := range labels {
		if !runnerLabelRe.MatchString(l) {
			return nil, "", ErrInvalidRunnerLabel
		}
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 100 {
		return nil, "", errors.New("runner name must be 1-100 characters")
	}
	if !strings.HasPrefix(regToken, registrationTokenPrefix) {
		return nil, "", ErrRegistrationToken
	}
	tok, err := s.runners.ConsumeRegistrationToken(ctx, hashSecret(regToken))
	if err != nil {
		return nil, "", err
	}
	if tok == nil {
		return nil, "", ErrRegistrationToken
	}
	raw, hash, err := newSecret(runnerTokenPrefix)
	if err != nil {
		return nil, "", err
	}
	r := &model.CIRunner{Scope: tok.Scope, RepoID: tok.RepoID, Name: name, Labels: labels, Version: version, CreatedBy: tok.CreatedBy}
	if err := s.runners.InsertRunner(ctx, r, hash); err != nil {
		return nil, "", err
	}
	return r, raw, nil
}

// Authenticate returns the runner a token belongs to and records the contact. A revoked runner fails here, so a
// revoke takes effect on its next request.
func (s *CIRunnerService) Authenticate(ctx context.Context, raw string) (*model.CIRunner, error) {
	if !strings.HasPrefix(raw, runnerTokenPrefix) {
		return nil, ErrRunnerUnauthorized
	}
	r, err := s.runners.RunnerByTokenHash(ctx, hashSecret(raw))
	if err != nil {
		return nil, err
	}
	if r == nil || r.RevokedAt != nil {
		return nil, ErrRunnerUnauthorized
	}
	now := time.Now()
	if r.LastContactAt == nil || now.Sub(*r.LastContactAt) >= contactTouchEvery {
		if err := s.runners.TouchRunner(ctx, r.ID, now); err != nil {
			return nil, err
		}
	}
	return r, nil
}

func (s *CIRunnerService) ListInstanceRunners(ctx context.Context) ([]model.CIRunner, error) {
	return s.runners.ListInstanceRunners(ctx)
}

func (s *CIRunnerService) ListRepoRunners(ctx context.Context, repoID int64) ([]model.CIRunner, error) {
	return s.runners.ListRepoRunners(ctx, repoID)
}

// GetRunner returns a runner by id for the management pages.
func (s *CIRunnerService) GetRunner(ctx context.Context, id int64) (*model.CIRunner, error) {
	return s.runners.GetRunner(ctx, id)
}

// Revoke cuts a runner off. The caller has checked the right to manage its scope.
func (s *CIRunnerService) Revoke(ctx context.Context, runnerID int64) error {
	return s.runners.Revoke(ctx, runnerID)
}

// Claim hands runner the oldest queued Job it may run, with a fresh per-job token, or nil when there is none.
// offered narrows the runner's registered labels; an empty list offers all of them.
func (s *CIRunnerService) Claim(ctx context.Context, runner *model.CIRunner, offered []string) (*runnerapi.JobSpec, error) {
	labels := runner.Labels
	if len(offered) > 0 {
		labels = nil
		for _, l := range offered {
			if slices.Contains(runner.Labels, l) {
				labels = append(labels, l)
			}
		}
		if len(labels) == 0 {
			return nil, nil
		}
	}
	raw, hash, err := newSecret(JobTokenPrefix)
	if err != nil {
		return nil, err
	}
	var spec *runnerapi.JobSpec
	job, err := s.runs.ClaimJobFor(ctx, labels, &runner.ID, runner.RepoID, hash, func(job *model.WorkflowJob) (err error) {
		spec, err = s.jobSpec(ctx, job, raw)
		return err
	})
	if err != nil || job == nil {
		return nil, err
	}
	return spec, nil
}

func (s *CIRunnerService) jobSpec(ctx context.Context, job *model.WorkflowJob, token string) (*runnerapi.JobSpec, error) {
	run, err := s.runs.GetRunByID(ctx, job.RunID)
	if err != nil {
		return nil, err
	}
	repo, err := s.repos.GetByID(ctx, run.RepoID)
	if err != nil {
		return nil, err
	}
	steps, err := s.runs.ListSteps(ctx, job.ID)
	if err != nil {
		return nil, err
	}
	siblings, err := s.runs.ListJobs(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	needs := map[string]string{}
	for _, n := range job.Needs {
		if res, _ := needResult(n, siblings); res != "" {
			needs[n] = res
		}
	}
	src, err := s.runSource(ctx, run)
	if err != nil {
		return nil, err
	}
	spec := &runnerapi.JobSpec{
		JobID: job.ID, RunID: run.ID, RunNumber: run.Number, Name: job.Name,
		Repository: repo.OwnerName + "/" + repo.Name, CloneURL: s.baseURL + "/" + src.OwnerName + "/" + src.Name + ".git",
		SHA: run.SHA, Ref: run.Ref, RunsOn: job.RunsOn, Container: job.Container, TimeoutMinutes: job.TimeoutMins,
		Env: job.Env, GitHub: s.runs.githubContext(ctx, run, repo), Matrix: job.Matrix, Needs: needs, Token: token,
	}
	if s.secrets != nil && !run.NoSecrets {
		if spec.Secrets, err = s.secrets.Values(ctx, run.RepoID); err != nil {
			return nil, err
		}
	}
	for _, st := range steps {
		spec.Steps = append(spec.Steps, runnerapi.StepSpec{Position: st.Position, Name: st.Name, Run: st.Run, Shell: st.Shell,
			WorkingDirectory: st.WorkingDir, Env: st.Env, If: st.IfExpr, TimeoutMinutes: st.TimeoutMin})
	}
	for _, a := range job.Artifacts {
		spec.Artifacts = append(spec.Artifacts, runnerapi.ArtifactSpec{Name: a.Name, Path: a.Path})
	}
	return spec, nil
}

// held loads a Job and checks that runner holds it.
func (s *CIRunnerService) held(ctx context.Context, runner *model.CIRunner, jobID int64) (*model.WorkflowJob, error) {
	job, err := s.runs.GetJob(ctx, jobID)
	if err != nil {
		return nil, ErrJobNotHeld
	}
	if job.RunnerID == nil || *job.RunnerID != runner.ID {
		return nil, ErrJobNotHeld
	}
	return job, nil
}

// Heartbeat renews the lease and reports whether the runner should carry on.
func (s *CIRunnerService) Heartbeat(ctx context.Context, runner *model.CIRunner, jobID int64) (bool, error) {
	if _, err := s.held(ctx, runner, jobID); err != nil {
		return false, err
	}
	return s.runs.RenewLease(ctx, jobID, runner.ID)
}

// SetStep records a Step's status for a Job the runner holds.
func (s *CIRunnerService) SetStep(ctx context.Context, runner *model.CIRunner, jobID int64, position int, status model.CIStatus) error {
	job, err := s.held(ctx, runner, jobID)
	if err != nil {
		return err
	}
	if job.Status != model.CIStatusRunning {
		return ErrRunFinished
	}
	return s.runs.SetStepStatus(ctx, jobID, position, status)
}

// Finish records the result of a Job the runner holds.
func (s *CIRunnerService) Finish(ctx context.Context, runner *model.CIRunner, jobID int64, result model.CIStatus, message string) error {
	if _, err := s.held(ctx, runner, jobID); err != nil {
		return err
	}
	return s.runs.FinishJob(ctx, jobID, result, message)
}

// runSource is the repository a Run's commit is cloned from: the Run's own, except for a fork pull request's Run, whose
// commit is in the head repository. A head that is gone falls back to the Run's repository, which holds a copy.
func (s *CIRunnerService) runSource(ctx context.Context, run *model.WorkflowRun) (*model.Repository, error) {
	if run.NoSecrets && run.PullRequestID != nil {
		if pr, err := s.runs.pulls.GetByID(ctx, *run.PullRequestID); err == nil && pr.CrossRepo && pr.HeadRepoID != nil {
			if head, err := s.repos.GetByID(ctx, *pr.HeadRepoID); err == nil {
				return head, nil
			}
		}
	}
	return s.repos.GetByID(ctx, run.RepoID)
}

// VerifyJobToken returns what a per-job token grants, or ErrRunnerUnauthorized once its Job is no longer running.
func (s *CIRunnerService) VerifyJobToken(ctx context.Context, raw string) (*JobToken, error) {
	if !strings.HasPrefix(raw, JobTokenPrefix) {
		return nil, ErrRunnerUnauthorized
	}
	job, err := s.runs.runs.RunningJobByTokenHash(ctx, hashSecret(raw))
	if err != nil {
		return nil, err
	}
	if job == nil {
		return nil, ErrRunnerUnauthorized
	}
	run, err := s.runs.GetRunByID(ctx, job.RunID)
	if err != nil {
		return nil, err
	}
	src, err := s.runSource(ctx, run)
	if err != nil {
		return nil, err
	}
	return &JobToken{JobID: job.ID, RunID: run.ID, RepoID: run.RepoID, CloneRepoID: src.ID, NoSecrets: run.NoSecrets}, nil
}

// Run fails the Jobs of silent runners every few seconds until ctx ends.
func (s *CIRunnerService) Run(ctx context.Context) {
	t := time.NewTicker(lostJobSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if n, err := s.runs.SweepLostJobs(ctx, time.Now()); err != nil {
				slog.Error("runner lost sweep failed", "error", err)
			} else if n > 0 {
				slog.Warn("jobs failed because their runner went silent", "jobs", n)
			}
		}
	}
}

// CurrentJobs maps each of runners that is running a Job to it.
func (s *CIRunnerService) CurrentJobs(ctx context.Context, runners []model.CIRunner) (map[int64]model.CIRunnerJob, error) {
	ids := make([]int64, len(runners))
	for i, r := range runners {
		ids[i] = r.ID
	}
	return s.runners.CurrentJobs(ctx, ids)
}

// ParseRunnerLabels splits a comma-separated label list and checks each label, so the command shown for a
// registration token is safe to paste into a shell.
func ParseRunnerLabels(s string) ([]string, error) {
	var out []string
	for _, l := range strings.Split(s, ",") {
		if l = strings.TrimSpace(l); l == "" {
			continue
		}
		if !runnerLabelRe.MatchString(l) {
			return nil, ErrInvalidRunnerLabel
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, ErrInvalidRunnerLabel
	}
	return out, nil
}
