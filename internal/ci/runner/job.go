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

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/logmask"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/workflow"
)

// API is the part of the protocol a running Job uses.
type API interface {
	Heartbeat(ctx context.Context, jobID int64) (bool, error)
	SetStep(ctx context.Context, jobID int64, position int, status string) error
	Finish(ctx context.Context, jobID int64, result, message string) error
	logSink
	UploadArtifact(ctx context.Context, jobID int64, token, name string, size int64, r io.Reader) error
}

// Executor runs claimed Jobs.
type Executor struct {
	Config   *Config
	Limits   Limits
	API      API
	Docker   string
	Checkout Checkout
	// Out also receives each Step's output; the server's copy is the log, so a nil Out is fine.
	Out io.Writer
	// Masker hides extra values before output leaves the runner; the Job's own secrets are always masked too.
	Masker *logmask.Masker
	// FlushEvery is how often streamed output is posted; a second unless a test shortens it.
	FlushEvery time.Duration
	// Minute is what a timeout-minutes of 1 lasts; tests shorten it.
	Minute time.Duration
	// HeartbeatEvery is runnerapi.HeartbeatEverySeconds unless a test shortens it.
	HeartbeatEvery time.Duration

	// held is the id of the Job being executed, 0 when idle; Sweep leaves its resources alone.
	held atomic.Int64
}

// idleSeconds keeps the Job's container alive until the runner removes it; busybox's sleep has no "infinity".
const idleSeconds = "2147483647"

var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (e *Executor) minutes(n int) time.Duration {
	if e.Minute == 0 {
		return time.Duration(n) * time.Minute
	}
	return time.Duration(n) * e.Minute
}

// Execute runs one Job to its end and reports the result. A Job the server cancelled mid-run is torn down without a
// result, since the server already ended it.
func (e *Executor) Execute(ctx context.Context, spec *runnerapi.JobSpec) {
	e.held.Store(spec.JobID)
	defer e.held.Store(0)
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
	network, volume := name, name

	defer func() {
		// A cancelled or timed-out Job still has to be cleaned up, so this does not use ctx.
		cctx, done := context.WithTimeout(context.Background(), time.Minute)
		defer done()
		_ = d.run(cctx, nil, "rm", "-f", name)
		_ = d.run(cctx, nil, "volume", "rm", "-f", volume)
		_ = d.run(cctx, nil, "network", "rm", network)
	}()

	if err := d.run(ctx, nil, append([]string{"network", "create"}, e.labels(spec.JobID, network)...)...); err != nil {
		return "failure", "could not create the job network: " + err.Error()
	}
	if err := d.run(ctx, nil, append([]string{"volume", "create"}, e.labels(spec.JobID, volume)...)...); err != nil {
		return "failure", "could not create the workspace volume: " + err.Error()
	}
	if err := e.populate(ctx, d, spec, image, network, volume, name); err != nil {
		return "failure", "checkout failed: " + err.Error()
	}

	// The log outlives ctx: output of a cancelled or timed-out Job is still worth posting while its token lives.
	masker := e.Masker
	if len(spec.Secrets) > 0 {
		masker = logmask.Combine(e.Masker, logmask.New(slices.Collect(maps.Values(spec.Secrets))))
	}
	logs := newLogStream(context.WithoutCancel(ctx), e.API, spec.JobID, spec.Token, masker, e.FlushEvery)
	defer logs.Close()

	failed := ""
	for _, st := range spec.Steps {
		if ctx.Err() != nil {
			break
		}
		skip, err := e.skipStep(spec, st, failed != "")
		if err != nil {
			e.setStep(ctx, spec, st.Position, "failure")
			if failed == "" {
				failed = fmt.Sprintf("step %d (%s): %v", st.Position+1, st.Name, err)
			}
			continue
		}
		if skip {
			e.setStep(ctx, spec, st.Position, "skipped")
			continue
		}
		e.setStep(ctx, spec, st.Position, "running")
		if err := e.runStep(ctx, d, spec, st, name, e.stepOut(logs.Writer(st.Position))); err != nil {
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
	// Artifacts are collected whether or not the Job passed: a failing test run's report is the one wanted most.
	// They go up before Finish because the per-job token dies with the Job.
	if err := e.uploadArtifacts(ctx, d, spec, name); err != nil && failed == "" {
		failed = err.Error()
	}
	if failed != "" {
		return "failure", failed
	}
	return "success", ""
}

func (e *Executor) stepOut(w io.Writer) io.Writer {
	if e.Out == nil {
		return w
	}
	return io.MultiWriter(w, e.Out)
}

func (e *Executor) setStep(ctx context.Context, spec *runnerapi.JobSpec, pos int, status string) {
	if err := e.API.SetStep(ctx, spec.JobID, pos, status); err != nil && ctx.Err() == nil {
		slog.Warn("report step status failed", "job", spec.JobID, "step", pos, "error", err)
	}
}

// populate clones the Job's commit on the host, creates the Job's one container, copies the checkout into its
// workspace volume and starts it. The container idles so each Step can `docker exec` into it and see what earlier
// Steps left behind.
func (e *Executor) populate(ctx context.Context, d docker, spec *runnerapi.JobSpec, image, network, volume, name string) error {
	dir, err := os.MkdirTemp("", "cz-runner-checkout-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	if err := e.Checkout(ctx, spec, dir); err != nil {
		return err
	}
	create := append(append([]string{"create", "--name", name}, e.labels(spec.JobID)...), e.Limits.hardening(network, volume)...)
	if err := d.run(ctx, nil, append(create, "--entrypoint=sleep", "--", image, idleSeconds)...); err != nil {
		return err
	}
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(tarWorkspace(dir, pw)) }()
	defer func() { _ = pr.Close() }()
	if err := d.run(ctx, pr, "cp", "-", name+":"+workspaceDir); err != nil {
		return err
	}
	return d.run(ctx, nil, "start", name)
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
	secrets := spec.Secrets
	if secrets == nil {
		secrets = map[string]string{}
	}
	return workflow.Context{GitHub: spec.GitHub, Env: env, Matrix: spec.Matrix, Secrets: secrets, Needs: spec.Needs, Status: status}
}

// stepEnv is the environment of a Step: the builtins, then the Job's env, then the Step's. The per-job token is not
// part of it, so workflow code never sees it.
func (e *Executor) stepEnv(spec *runnerapi.JobSpec, st runnerapi.StepSpec) (map[string]string, error) {
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
	return env, nil
}

// envFile renders env as `export` lines for the shell to source. Single quotes keep every byte literal, newlines
// included.
func envFile(env map[string]string) string {
	var b strings.Builder
	for _, k := range slices.Sorted(maps.Keys(env)) {
		b.WriteString("export " + k + "='" + strings.ReplaceAll(env[k], "'", `'\''`) + "'\n")
	}
	return b.String()
}

// shellCommand is the interpreter and its flags for running a script file.
func shellCommand(shell string) (entrypoint string, args []string, err error) {
	switch shell {
	case "", "sh":
		return "sh", []string{"-e"}, nil
	case "bash":
		return "bash", []string{"--noprofile", "--norc", "-eo", "pipefail"}, nil
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

// stepDir holds a Step's env file and script inside the container. They travel over stdin and the volume rather than
// argv, which any local user of the runner host can read from ps while the Step runs.
const stepDir = homeDir + "/.cz-step"

// stepRun is one Step ready to exec: the files to put in the container and the `docker exec` argument list. exec
// inherits the container's user, limits, network and volume, so it takes no flag that could loosen them.
type stepRun struct {
	envPath, envBody       string
	scriptPath, scriptBody string
	args                   []string
}

// stepWrapper sources and removes the env file, runs the interpreter on the script and removes the script.
const stepWrapper = `env=$1; scr=$2; shift 2; . "$env"; rm -f "$env"; "$@" "$scr"; rc=$?; rm -f "$scr"; exit $rc`

func (e *Executor) stepRun(spec *runnerapi.JobSpec, st runnerapi.StepSpec, container string) (*stepRun, error) {
	script, err := workflow.Interpolate(st.Run, e.exprContext(spec, st, "success"))
	if err != nil {
		return nil, err
	}
	env, err := e.stepEnv(spec, st)
	if err != nil {
		return nil, err
	}
	wd, err := workdir(st.WorkingDirectory)
	if err != nil {
		return nil, err
	}
	entry, shellArgs, err := shellCommand(st.Shell)
	if err != nil {
		return nil, err
	}
	r := &stepRun{
		envPath: fmt.Sprintf("%s/%d.env", stepDir, st.Position), envBody: envFile(env),
		scriptPath: fmt.Sprintf("%s/%d.sh", stepDir, st.Position), scriptBody: script,
	}
	r.args = append([]string{"exec", "--workdir=" + wd, "--", container, "sh", "-c", stepWrapper, "sh", r.envPath, r.scriptPath, entry}, shellArgs...)
	return r, nil
}

// put writes body to path inside the container, readable by the Job's user only.
func (d docker) put(ctx context.Context, container, path, body string) error {
	return d.run(ctx, strings.NewReader(body), "exec", "-i", "--", container, "sh", "-c", `umask 077; mkdir -p "${1%/*}" && cat > "$1"`, "sh", path)
}

func (e *Executor) runStep(ctx context.Context, d docker, spec *runnerapi.JobSpec, st runnerapi.StepSpec, container string, out io.Writer) error {
	run, err := e.stepRun(spec, st, container)
	if err != nil {
		return err
	}
	if err := d.put(ctx, container, run.envPath, run.envBody); err != nil {
		return err
	}
	if err := d.put(ctx, container, run.scriptPath, run.scriptBody); err != nil {
		return err
	}
	args := run.args
	stepCtx := ctx
	if st.TimeoutMinutes > 0 {
		var cancel context.CancelFunc
		stepCtx, cancel = context.WithTimeout(ctx, e.minutes(st.TimeoutMinutes))
		defer cancel()
	}
	// Not CommandContext: killing the docker CLI would leave the Step's processes running in the container.
	c := d.cmd(context.Background(), args...)
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
		// SIGKILL to every process but the container's PID 1, so the Job survives its Step timing out.
		_ = d.run(kill, nil, "exec", "--", container, "sh", "-c", "kill -9 -1")
		_ = c.Process.Kill()
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
