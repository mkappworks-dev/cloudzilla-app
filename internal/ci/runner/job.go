package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"os/exec"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/workflow"
)

// API is the part of the protocol a running Job uses.
type API interface {
	Heartbeat(ctx context.Context, jobID int64) (bool, error)
	SetStep(ctx context.Context, jobID int64, position int, status string) error
	Finish(ctx context.Context, jobID int64, result, message string) error
}

// Executor runs claimed Jobs.
type Executor struct {
	Config   *Config
	Limits   Limits
	API      API
	Docker   string
	Checkout Checkout
	// Out receives each Step's output.
	Out io.Writer
	// Minute is what a timeout-minutes of 1 lasts; tests shorten it.
	Minute time.Duration
	// HeartbeatEvery is runnerapi.HeartbeatEverySeconds unless a test shortens it.
	HeartbeatEvery time.Duration
}

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Execute runs one Job to its end and reports the result. A Job the server cancelled mid-run is torn down without a
// result, since the server already ended it.
func (e *Executor) minutes(n int) time.Duration {
	if e.Minute == 0 {
		return time.Duration(n) * time.Minute
	}
	return time.Duration(n) * e.Minute
}

func (e *Executor) Execute(ctx context.Context, spec *runnerapi.JobSpec) {
	jobCtx, cancel := context.WithTimeout(ctx, e.minutes(spec.TimeoutMinutes))
	defer cancel()

	var stopped atomic.Bool
	hbDone := make(chan struct{})
	go func() {
		defer close(hbDone)
		e.heartbeat(jobCtx, spec.JobID, func() { stopped.Store(true); cancel() })
	}()

	result, msg := e.run(jobCtx, spec)
	cancel()
	<-hbDone

	if stopped.Load() {
		slog.Info("job was cancelled by the server", "job", spec.JobID)
		return
	}
	switch {
	case errors.Is(context.Cause(jobCtx), context.DeadlineExceeded) && result != "success":
		result, msg = "failure", fmt.Sprintf("job timed out after %d minutes", spec.TimeoutMinutes)
	case ctx.Err() != nil:
		result, msg = "cancelled", "runner stopped"
	}
	fin, done := context.WithTimeout(context.Background(), 15*time.Second)
	defer done()
	if err := e.API.Finish(fin, spec.JobID, result, msg); err != nil {
		slog.Error("report job result failed", "job", spec.JobID, "error", err)
	}
}

func (e *Executor) heartbeat(ctx context.Context, jobID int64, stop func()) {
	every := e.HeartbeatEvery
	if every == 0 {
		every = runnerapi.HeartbeatEverySeconds * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			cont, err := e.API.Heartbeat(ctx, jobID)
			switch {
			case errors.Is(err, ErrUnauthorized):
				stop()
				return
			case err != nil:
				// A missed beat is survivable: the server waits 60 s.
				slog.Warn("heartbeat failed", "job", jobID, "error", err)
			case !cont:
				stop()
				return
			}
		}
	}
}

// run prepares the Job's network and workspace, runs its Steps and always removes what it made.
func (e *Executor) run(ctx context.Context, spec *runnerapi.JobSpec) (string, string) {
	if err := e.Limits.Validate(); err != nil {
		return "failure", err.Error()
	}
	image, err := e.Config.ResolveImage(spec.Container, spec.RunsOn)
	if err != nil {
		return "failure", err.Error()
	}
	d := docker{bin: e.Docker}
	name := fmt.Sprintf("cz-job-%d", spec.JobID)
	network, volume, initName := name, name, name+"-init"

	defer func() {
		// A cancelled or timed-out Job still has to be cleaned up, so this does not use ctx.
		cctx, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		_ = d.run(cctx, nil, "rm", "-f", initName)
		_ = d.run(cctx, nil, "volume", "rm", "-f", volume)
		_ = d.run(cctx, nil, "network", "rm", network)
	}()

	if err := d.run(ctx, nil, "network", "create", network); err != nil {
		return "failure", "could not create the job network: " + err.Error()
	}
	if err := d.run(ctx, nil, "volume", "create", volume); err != nil {
		return "failure", "could not create the workspace volume: " + err.Error()
	}
	if err := e.populate(ctx, d, spec, image, network, volume, initName); err != nil {
		return "failure", "checkout failed: " + err.Error()
	}

	failed := ""
	for _, st := range spec.Steps {
		if ctx.Err() != nil {
			break
		}
		skip, err := e.skipStep(spec, st, failed != "")
		if err != nil {
			e.setStep(ctx, spec, st.Position, "failure")
			failed = fmt.Sprintf("step %d (%s): %v", st.Position+1, st.Name, err)
			continue
		}
		if skip {
			e.setStep(ctx, spec, st.Position, "skipped")
			continue
		}
		e.setStep(ctx, spec, st.Position, "running")
		if err := e.runStep(ctx, d, spec, st, image, network, volume); err != nil {
			e.setStep(ctx, spec, st.Position, "failure")
			if failed == "" {
				failed = fmt.Sprintf("step %d (%s): %v", st.Position+1, st.Name, err)
			}
			continue
		}
		e.setStep(ctx, spec, st.Position, "success")
	}
	if ctx.Err() != nil {
		return "cancelled", ctx.Err().Error()
	}
	if failed != "" {
		return "failure", failed
	}
	return "success", ""
}

func (e *Executor) setStep(ctx context.Context, spec *runnerapi.JobSpec, pos int, status string) {
	if err := e.API.SetStep(ctx, spec.JobID, pos, status); err != nil && ctx.Err() == nil {
		slog.Warn("report step status failed", "job", spec.JobID, "step", pos, "error", err)
	}
}

// populate clones the Job's commit on the host and copies it into the workspace volume through a container that is
// created but never started.
func (e *Executor) populate(ctx context.Context, d docker, spec *runnerapi.JobSpec, image, network, volume, initName string) error {
	dir, err := os.MkdirTemp("", "cz-runner-checkout-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := e.Checkout(ctx, spec, dir); err != nil {
		return err
	}
	create := append([]string{"create", "--name", initName}, e.Limits.hardening(network, volume)...)
	if err := d.run(ctx, nil, append(create, "--", image, "true")...); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(tarWorkspace(dir, pw)) }()
	defer func() { _ = pr.Close() }()
	return d.run(ctx, pr, "cp", "-", initName+":"+workspaceDir)
}

// skipStep evaluates a Step's `if` (success() when it has none). A Step whose condition cannot be evaluated fails.
func (e *Executor) skipStep(spec *runnerapi.JobSpec, st runnerapi.StepSpec, failedBefore bool) (bool, error) {
	cond := st.If
	if cond == "" {
		cond = "success()"
	}
	expr, err := workflow.ParseExpr(cond)
	if err != nil {
		return false, err
	}
	status := "success"
	if failedBefore {
		status = "failure"
	}
	ok, err := expr.Bool(e.exprContext(spec, st, status))
	return !ok, err
}

func (e *Executor) exprContext(spec *runnerapi.JobSpec, st runnerapi.StepSpec, status string) workflow.Context {
	env := maps.Clone(spec.Env)
	if env == nil {
		env = map[string]string{}
	}
	maps.Copy(env, st.Env)
	return workflow.Context{GitHub: spec.GitHub, Env: env, Matrix: spec.Matrix, Needs: spec.Needs, Status: status}
}

// stepEnv is the environment of a Step: the builtins, then the Job's env, then the Step's. The per-job token is not
// part of it, so workflow code never sees it.
func (e *Executor) stepEnv(spec *runnerapi.JobSpec, st runnerapi.StepSpec) ([]string, error) {
	ctx := e.exprContext(spec, st, "success")
	env := map[string]string{
		"CI": "true", "CLOUDZILLA_ACTIONS": "true", "GITHUB_WORKSPACE": checkoutDir, "HOME": homeDir,
		"GITHUB_SHA": spec.SHA, "GITHUB_REF": spec.Ref, "GITHUB_REPOSITORY": spec.Repository,
		"GITHUB_REF_NAME": spec.GitHub["ref_name"], "GITHUB_EVENT_NAME": spec.GitHub["event_name"],
		"GITHUB_ACTOR": spec.GitHub["actor"], "GITHUB_RUN_NUMBER": spec.GitHub["run_number"],
	}
	for _, layer := range []map[string]string{spec.Env, st.Env} {
		for k, v := range layer {
			if !envKeyRe.MatchString(k) {
				return nil, fmt.Errorf("invalid environment variable name %q", k)
			}
			out, err := workflow.Interpolate(v, ctx)
			if err != nil {
				return nil, fmt.Errorf("env %s: %w", k, err)
			}
			env[k] = out
		}
	}
	args := make([]string, 0, 2*len(env))
	for _, k := range slices.Sorted(maps.Keys(env)) {
		args = append(args, "--env="+k+"="+env[k])
	}
	return args, nil
}

// shellCommand is the entrypoint and arguments that run script. The script is one argv element, so it is never
// parsed by anything but the shell inside the container.
func shellCommand(shell, script string) (entrypoint string, args []string, err error) {
	switch shell {
	case "", "sh":
		return "sh", []string{"-e", "-c", script}, nil
	case "bash":
		return "bash", []string{"--noprofile", "--norc", "-eo", "pipefail", "-c", script}, nil
	}
	return "", nil, fmt.Errorf("unsupported shell %q", shell)
}

// workdir is the Step's working directory inside the workspace; it may not leave it.
func workdir(rel string) (string, error) {
	if rel == "" {
		return checkoutDir, nil
	}
	clean := path.Clean(rel)
	if path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("working-directory %q must stay inside the workspace", rel)
	}
	return path.Join(checkoutDir, clean), nil
}

// stepArgs is the full `docker run` argument list for one Step.
func (e *Executor) stepArgs(spec *runnerapi.JobSpec, st runnerapi.StepSpec, image, network, volume, container string) ([]string, error) {
	script, err := workflow.Interpolate(st.Run, e.exprContext(spec, st, "success"))
	if err != nil {
		return nil, err
	}
	envArgs, err := e.stepEnv(spec, st)
	if err != nil {
		return nil, err
	}
	wd, err := workdir(st.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	entry, shellArgs, err := shellCommand(st.Shell, script)
	if err != nil {
		return nil, err
	}
	args := []string{"run", "--rm", "--name", container}
	args = append(args, e.Limits.hardening(network, volume)...)
	args = append(args, "--workdir="+wd, "--entrypoint="+entry)
	args = append(args, envArgs...)
	args = append(args, "--", image)
	return append(args, shellArgs...), nil
}

func (e *Executor) runStep(ctx context.Context, d docker, spec *runnerapi.JobSpec, st runnerapi.StepSpec, image, network, volume string) error {
	container := fmt.Sprintf("cz-job-%d-step-%d", spec.JobID, st.Position)
	args, err := e.stepArgs(spec, st, image, network, volume, container)
	if err != nil {
		return err
	}
	stepCtx := ctx
	if st.TimeoutMinutes > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, e.minutes(st.TimeoutMinutes))
		defer cancel()
	}
	// Not CommandContext: killing the docker CLI would leave the container running, so the container is removed
	// by name instead.
	c := d.cmd(context.Background(), args...)
	out := e.Out
	if out == nil {
		out = os.Stdout
	}
	c.Stdout, c.Stderr = out, out
	if err := c.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- c.Wait() }()
	select {
	case err := <-done:
		return exitError(err)
	case <-stepCtx.Done():
		kill, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = d.run(kill, nil, "rm", "-f", container)
		<-done
		if errors.Is(stepCtx.Err(), context.DeadlineExceeded) && ctx.Err() == nil {
			return fmt.Errorf("timed out after %d minutes", st.TimeoutMinutes)
		}
		return stepCtx.Err()
	}
}

func exitError(err error) error {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return fmt.Errorf("exited with code %d", ee.ExitCode())
	}
	return err
}
