package runner

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/logmask"
	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// stubScript stands in for the docker CLI: it logs each invocation's arguments (NUL separated, records ended by
// \001) and succeeds, except that an `exec` whose arguments mention STUBFAIL exits 3 and one that mentions STUBHANG
// blocks until `rm -f` or a `kill -9 -1` exec ends it, as the real CLI would exit when its process dies.
const stubScript = `#!/bin/sh
printf '%s\0' "$@" >> "$STUB_DIR/log"
printf '\001' >> "$STUB_DIR/log"
case "$1" in
  cp)
    if [ "$2" = "-" ]; then cat > /dev/null
    elif [ -e "$STUB_DIR/artifact.tar" ]; then cat "$STUB_DIR/artifact.tar"
    else echo "Could not find the file $2" >&2; exit 1; fi ;;
  rm) for a in "$@"; do last=$a; done; : > "$STUB_DIR/removed-$last" ;;
  exec)
    name=""; prev=""
    for a in "$@"; do [ "$prev" = "--" ] && name=$a; prev=$a; done
    case "$*" in *"kill -9 -1"*) : > "$STUB_DIR/removed-$name"; exit 0 ;; esac
    case " $* " in *" -i "*) mkdir -p "$STUB_DIR/files"; for a in "$@"; do last=$a; done; cat > "$STUB_DIR/files/$(basename "$last")"; exit 0 ;; esac
    content=""
    for a in "$@"; do case "$a" in */.cz-step/*) [ -e "$STUB_DIR/files/$(basename "$a")" ] && content="$content $(cat "$STUB_DIR/files/$(basename "$a")")" ;; esac; done
    set -- "$@" "$content"
    case "$*" in *STUBFAIL*) echo "boom" >&2; exit 3 ;; esac
    case "$*" in *STUBTAIL*) echo "first line"; while [ ! -e "$STUB_DIR/removed-$name" ]; do sleep 0.02; done; exit 137 ;; esac
    case "$*" in *STUBHANG*) while [ ! -e "$STUB_DIR/removed-$name" ]; do sleep 0.02; done; exit 137 ;; esac
    echo "ran in $name" ;;
esac
exit 0
`

type call []string

func (c call) has(a string) bool { return slices.Contains(c, a) }

func stubDocker(t *testing.T) (readCalls func() []call) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "docker")
	if err := os.WriteFile(bin, []byte(stubScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("STUB_DIR", dir)
	return func() []call {
		data, _ := os.ReadFile(filepath.Join(dir, "log"))
		var out []call
		for _, rec := range strings.Split(string(data), "\x01") {
			if rec == "" {
				continue
			}
			out = append(out, strings.Split(strings.TrimSuffix(rec, "\x00"), "\x00"))
		}
		return out
	}
}

type postedLog struct {
	step, seq int
	text      string
}

type fakeAPI struct {
	mu        sync.Mutex
	logs      []postedLog
	artifacts map[string][]byte
	artErr    error
	truncate  bool
	steps     []string
	result    string
	message   string
	finished  bool
	heartbeat func() (bool, error)
}

func (f *fakeAPI) Heartbeat(context.Context, int64) (bool, error) {
	if f.heartbeat != nil {
		return f.heartbeat()
	}
	return true, nil
}

func (f *fakeAPI) SetStep(_ context.Context, _ int64, pos int, status string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.steps = append(f.steps, string(rune('0'+pos))+":"+status)
	return nil
}

func (f *fakeAPI) Finish(_ context.Context, _ int64, result, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished, f.result, f.message = true, result, message
	return nil
}

func (f *fakeAPI) PostLog(_ context.Context, _ int64, token string, step, seq int, data []byte) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if token != "czj_secret" {
		return false, ErrUnauthorized
	}
	f.logs = append(f.logs, postedLog{step, seq, string(data)})
	return f.truncate, nil
}

func (f *fakeAPI) UploadArtifact(_ context.Context, _ int64, token, name string, size int64, r io.Reader) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	if int64(len(b)) != size || token != "czj_secret" {
		return errors.New("bad upload")
	}
	if f.artErr != nil {
		return f.artErr
	}
	if f.artifacts == nil {
		f.artifacts = map[string][]byte{}
	}
	f.artifacts[name] = b
	return nil
}

func (f *fakeAPI) postedText(step int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b strings.Builder
	for _, l := range f.logs {
		if l.step == step {
			b.WriteString(l.text)
		}
	}
	return b.String()
}

func newExecutor(api *fakeAPI) *Executor {
	return &Executor{
		Config: &Config{Images: map[string]string{"linux": "ubuntu:24.04"}},
		Limits: DefaultLimits(), API: api, HeartbeatEvery: 10 * time.Millisecond, FlushEvery: 20 * time.Millisecond,
		Checkout: func(_ context.Context, _ *runnerapi.JobSpec, dir string) error {
			return os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0o644)
		},
	}
}

func jobSpec(steps ...runnerapi.StepSpec) *runnerapi.JobSpec {
	for i := range steps {
		steps[i].Position = i
		if steps[i].Name == "" {
			steps[i].Name = "step"
		}
	}
	return &runnerapi.JobSpec{JobID: 7, RunNumber: 1, Name: "test", Repository: "o/r", SHA: "abc", Ref: "refs/heads/main",
		RunsOn: []string{"linux"}, TimeoutMinutes: 5, Steps: steps, Token: "czj_secret",
		GitHub: map[string]string{"sha": "abc", "ref_name": "main", "event_name": "push", "actor": "u", "run_number": "1"}}
}

var wantHardening = []string{
	"--privileged=false", "--cap-drop=ALL", "--security-opt=no-new-privileges", "--user=1000:1000", "--cpus=2", "--memory=4g",
	"--memory-swap=4g", "--pids-limit=512", "--network=cz-job-7", "--volume=cz-job-7:/workspace",
}

func stubFile(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(os.Getenv("STUB_DIR"), "files", name))
	if err != nil {
		t.Fatalf("stub file %s: %v", name, err)
	}
	return string(b)
}

func execs(calls []call) []call {
	var out []call
	for _, c := range calls {
		if c[0] == "exec" && !c.has("-i") {
			out = append(out, c)
		}
	}
	return out
}

func containerStarts(calls []call) []call {
	var out []call
	for _, c := range calls {
		if c[0] == "run" || c[0] == "create" {
			out = append(out, c)
		}
	}
	return out
}

func TestExecute_EveryContainerCarriesTheHardeningFlags(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	spec := jobSpec(runnerapi.StepSpec{Run: "go build ./..."}, runnerapi.StepSpec{Run: "echo ${{ github.sha }}", Shell: "bash", WorkingDirectory: "sub/dir",
		Env: map[string]string{"B": "2", "A": "1"}})
	spec.Container = "golang:1.23"
	spec.Env = map[string]string{"GOFLAGS": "-mod=mod"}

	e.Execute(context.Background(), spec)

	if !api.finished || api.result != "success" {
		t.Fatalf("result = %q %q", api.result, api.message)
	}
	cs := calls()
	starts := containerStarts(cs)
	if len(starts) != 1 || starts[0][0] != "create" {
		t.Fatalf("container starts = %v, want exactly one create for the whole Job", starts)
	}
	for _, f := range wantHardening {
		if !starts[0].has(f) {
			t.Errorf("create lacks %s", f)
		}
	}
	for _, c := range cs {
		for _, a := range c {
			if a == "--privileged" || strings.HasPrefix(a, "--cap-add") || strings.HasPrefix(a, "--device") || strings.HasPrefix(a, "--pid=") || strings.HasPrefix(a, "--pid ") ||
				strings.HasPrefix(a, "--ipc") || strings.HasPrefix(a, "--userns") || strings.Contains(a, "docker.sock") || a == "-v" ||
				(strings.HasPrefix(a, "--volume=") && !strings.HasPrefix(a, "--volume=cz-job-7:")) {
				t.Errorf("%s has a loosening argument %q", c[0], a)
			}
		}
	}
	for _, c := range execs(cs) {
		for _, a := range c {
			if a == "--" {
				break
			}
			if !strings.HasPrefix(a, "--workdir=") && a != "exec" {
				t.Errorf("exec carries an unexpected flag %q", a)
			}
		}
	}
	wantCreate := append(append([]string{"create", "--name", "cz-job-7"}, wantHardening...), "--entrypoint=sleep", "--", "golang:1.23", "2147483647")
	if !slices.Equal(starts[0], wantCreate) {
		t.Errorf("create args =\n%q\nwant\n%q", starts[0], wantCreate)
	}
	ex := execs(cs)
	if len(ex) != 2 {
		t.Fatalf("execs = %d, want one per step: %v", len(ex), ex)
	}
	wantFirst := []string{"exec", "--workdir=/workspace/src", "--", "cz-job-7", "sh", "-c", stepWrapper, "sh",
		"/workspace/home/.cz-step/0.env", "/workspace/home/.cz-step/0.sh", "sh", "-e"}
	if !slices.Equal(ex[0], wantFirst) {
		t.Errorf("step 0 args =\n%q\nwant\n%q", ex[0], wantFirst)
	}
	if got := stubFile(t, "0.sh"); got != "go build ./..." {
		t.Errorf("step 0 script = %q", got)
	}
	for _, want := range []string{"export CI='true'\n", "export GITHUB_SHA='abc'\n", "export GOFLAGS='-mod=mod'\n", "export HOME='/workspace/home'\n"} {
		if !strings.Contains(stubFile(t, "0.env"), want) {
			t.Errorf("step 0 env file lacks %q: %q", want, stubFile(t, "0.env"))
		}
	}
	second := ex[1]
	if !second.has("--workdir=/workspace/src/sub/dir") || !second.has("bash") || stubFile(t, "1.sh") != "echo abc" ||
		!strings.Contains(stubFile(t, "1.env"), "export A='1'\n") || !strings.Contains(stubFile(t, "1.env"), "export B='2'\n") {
		t.Errorf("step 1 args = %q", second)
	}
	if !slices.Equal(api.steps, []string{"0:running", "0:success", "1:running", "1:success"}) {
		t.Errorf("step statuses = %v", api.steps)
	}
	for _, c := range calls() {
		if slices.ContainsFunc(c, func(a string) bool { return strings.Contains(a, "czj_secret") }) {
			t.Errorf("per-job token leaked into docker %s", c[0])
		}
	}
}

func TestExecute_NothingInAWorkflowCanLoosenTheFlags(t *testing.T) {
	for name, tc := range map[string]struct {
		step      runnerapi.StepSpec
		container string
		wantMsg   string
	}{
		"env key that is a flag": {step: runnerapi.StepSpec{Run: "x", Env: map[string]string{"--privileged": "1"}}, wantMsg: "invalid environment variable name"},
		"working directory out":  {step: runnerapi.StepSpec{Run: "x", WorkingDirectory: "../.."}, wantMsg: "inside the workspace"},
		"absolute working dir":   {step: runnerapi.StepSpec{Run: "x", WorkingDirectory: "/etc"}, wantMsg: "inside the workspace"},
		"unknown shell":          {step: runnerapi.StepSpec{Run: "x", Shell: "pwsh"}, wantMsg: "unsupported shell"},
		"image that is a flag":   {step: runnerapi.StepSpec{Run: "x"}, container: "--privileged", wantMsg: "not a valid image"},
		"image with a space":     {step: runnerapi.StepSpec{Run: "x"}, container: "alpine --privileged", wantMsg: "not a valid image"},
	} {
		t.Run(name, func(t *testing.T) {
			calls := stubDocker(t)
			api := &fakeAPI{}
			spec := jobSpec(tc.step)
			spec.Container = tc.container
			newExecutor(api).Execute(context.Background(), spec)
			if api.result != "failure" || !strings.Contains(api.message, tc.wantMsg) {
				t.Errorf("result = %q %q, want failure containing %q", api.result, api.message, tc.wantMsg)
			}
			for _, c := range calls() {
				if c[0] == "exec" {
					t.Errorf("a step ran: %q", c)
				}
				for _, a := range c {
					if a == "--privileged" {
						t.Errorf("flag injected: %q", c)
					}
				}
			}
		})
	}
}

func TestExecute_ImageOutsideAllowedImagesFailsTheJobWithTheReason(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	e.Config.AllowedImages = []string{"golang:*", "node:22"}
	spec := jobSpec(runnerapi.StepSpec{Run: "x"})
	spec.Container = "evil/miner:latest"

	e.Execute(context.Background(), spec)

	if api.result != "failure" || !strings.Contains(api.message, `"evil/miner:latest" is not in this runner's allowed_images`) {
		t.Errorf("result = %q %q", api.result, api.message)
	}
	if n := len(calls()); n != 0 {
		t.Errorf("docker was called %d times for a refused image", n)
	}

	for _, ok := range []string{"golang:1.23", "node:22", "ubuntu:24.04"} {
		if _, err := e.Config.ResolveImage(ok, nil); err != nil {
			t.Errorf("image %s refused: %v", ok, err)
		}
	}
}

func TestExecute_LabelPicksTheDefaultImage(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "x"}))
	if starts := containerStarts(calls()); len(starts) != 1 || !starts[0].has("ubuntu:24.04") {
		t.Errorf("starts = %q", starts)
	}

	api = &fakeAPI{}
	spec := jobSpec(runnerapi.StepSpec{Run: "x"})
	spec.RunsOn = []string{"gpu"}
	newExecutor(api).Execute(context.Background(), spec)
	if api.result != "failure" || !strings.Contains(api.message, "no image for labels gpu") {
		t.Errorf("unmapped label = %q %q", api.result, api.message)
	}
}

func TestExecute_FailedStepFailsTheJobAndLaterStepsFollowTheirIf(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	spec := jobSpec(
		runnerapi.StepSpec{Name: "build", Run: "STUBFAIL"},
		runnerapi.StepSpec{Name: "test", Run: "echo skipped"},
		runnerapi.StepSpec{Name: "report", Run: "echo report", If: "failure()"},
		runnerapi.StepSpec{Name: "cleanup", Run: "echo cleanup", If: "always()"},
	)
	newExecutor(api).Execute(context.Background(), spec)

	if api.result != "failure" || api.message != "step 1 (build): exited with code 3" {
		t.Errorf("result = %q %q", api.result, api.message)
	}
	want := []string{"0:running", "0:failure", "1:skipped", "2:running", "2:success", "3:running", "3:success"}
	if !slices.Equal(api.steps, want) {
		t.Errorf("steps = %v, want %v", api.steps, want)
	}
}

func TestExecute_RemovesWhatItMadeEvenWhenAStepFails(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "STUBFAIL"}))
	var tail []string
	for _, c := range calls() {
		if c[0] == "rm" || (len(c) > 1 && c[1] == "rm") {
			tail = append(tail, strings.Join(c, " "))
		}
	}
	want := []string{"rm -f cz-job-7", "volume rm -f cz-job-7", "network rm cz-job-7"}
	if !slices.Equal(tail, want) {
		t.Errorf("cleanup = %q, want %q", tail, want)
	}
}

func TestExecute_JobCancelledByTheServerStopsTheContainerAndReportsNothing(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	api.heartbeat = func() (bool, error) {
		return !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "exec" && !c.has("-i") }), nil
	}

	done := make(chan struct{})
	go func() {
		newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "STUBHANG"}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a cancelled job kept running")
	}
	if api.finished {
		t.Errorf("a cancelled job reported %q", api.result)
	}
	removedStep := false
	for _, c := range calls() {
		if c[0] == "exec" && strings.Contains(strings.Join(c, " "), "kill -9 -1") {
			removedStep = true
		}
	}
	if !removedStep {
		t.Error("the step container was not removed")
	}
}

func TestExecute_RevokedTokenOnHeartbeatStopsTheJob(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	api.heartbeat = func() (bool, error) { return false, ErrUnauthorized }
	done := make(chan struct{})
	go func() {
		newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "STUBHANG"}))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("job outlived its revoked token")
	}
}

func TestExecute_StoppingTheRunnerKillsTheContainerAndReportsCancelled(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	e.HeartbeatEvery = time.Hour
	spec := jobSpec(runnerapi.StepSpec{Name: "hang", Run: "STUBHANG"})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		for !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "exec" && !c.has("-i") }) {
			time.Sleep(10 * time.Millisecond)
		}
		cancel()
	}()
	e.Execute(ctx, spec)
	if !api.finished || api.result != "cancelled" {
		t.Errorf("result = %q %q", api.result, api.message)
	}
	killed := false
	for _, c := range calls() {
		killed = killed || (c[0] == "exec" && strings.Contains(strings.Join(c, " "), "kill -9 -1"))
	}
	if !killed {
		t.Error("container was not removed on stop")
	}
}

func TestExecute_StepsOfAJobShareOneContainer(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "apt-get install -y jq"}, runnerapi.StepSpec{Run: "jq --version"}))
	ex := execs(calls())
	if len(ex) != 2 {
		t.Fatalf("execs = %d", len(ex))
	}
	for _, c := range ex {
		if !c.has("cz-job-7") {
			t.Errorf("step ran outside the job container: %q", c)
		}
	}
	if n := len(containerStarts(calls())); n != 1 {
		t.Errorf("container creations = %d, want 1", n)
	}
}

func TestLimits_RefuseRoot(t *testing.T) {
	for _, u := range []string{"", "0", "0:0", "root", "root:root"} {
		l := DefaultLimits()
		l.User = u
		if l.Validate() == nil {
			t.Errorf("user %q accepted", u)
		}
	}
	if err := DefaultLimits().Validate(); err != nil {
		t.Errorf("defaults rejected: %v", err)
	}
}

func TestTarWorkspace_EntriesAreWorldWritable(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh"), 0o755)
	var buf bytes.Buffer
	if err := tarWorkspace(dir, &buf); err != nil {
		t.Fatal(err)
	}
	modes := map[string]int64{}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err != nil {
			break
		}
		modes[h.Name] = h.Mode
	}
	want := map[string]int64{"home/": 0o777, "src/": 0o777, "src/sub/": 0o777, "src/sub/a.txt": 0o666, "src/run.sh": 0o777}
	for n, m := range want {
		if modes[n] != m {
			t.Errorf("%s mode = %o, want %o", n, modes[n], m)
		}
	}
}

func TestConfig_SaveIsPrivateAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "config.yaml")
	c := &Config{Server: "https://x", Token: "czrun_t", Name: "n", Labels: []string{"linux"}, Images: map[string]string{"linux": "ubuntu:24.04"}}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o", fi.Mode().Perm())
	}
	got, err := LoadConfig(path)
	if err != nil || got.Token != "czrun_t" || got.Images["linux"] != "ubuntu:24.04" {
		t.Errorf("loaded %+v, %v", got, err)
	}
	if _, err := LoadConfig(filepath.Join(t.TempDir(), "missing")); err == nil || errors.Is(err, nil) {
		t.Error("missing config loaded")
	}
}

func TestExecute_TimeoutMinutesOfAStepAndOfTheJobAreHonoured(t *testing.T) {
	for name, tc := range map[string]struct {
		jobMin, stepMin int
		wantMsg         string
	}{
		"step": {jobMin: 1000, stepMin: 1, wantMsg: "step 1 (hang): timed out after 1 minutes"},
		"job":  {jobMin: 2, stepMin: 0, wantMsg: "job timed out after 2 minutes"},
	} {
		t.Run(name, func(t *testing.T) {
			calls := stubDocker(t)
			api := &fakeAPI{}
			e := newExecutor(api)
			e.Minute = 100 * time.Millisecond
			e.HeartbeatEvery = time.Hour
			spec := jobSpec(runnerapi.StepSpec{Name: "hang", Run: "STUBHANG", TimeoutMinutes: tc.stepMin})
			spec.TimeoutMinutes = tc.jobMin
			e.Execute(context.Background(), spec)
			if api.result != "failure" || api.message != tc.wantMsg {
				t.Errorf("result = %q %q, want failure %q", api.result, api.message, tc.wantMsg)
			}
			if !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "exec" && strings.Contains(strings.Join(c, " "), "kill -9 -1") }) {
				t.Error("container was not removed")
			}
		})
	}
}

func TestExecute_OutputIsPostedPerStepInOrderWithSecretsMasked(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	e.Masker = logmask.New([]string{"cz-job-7"})
	e.Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "one"}, runnerapi.StepSpec{Run: "two"}))

	if got := api.postedText(0); got != "ran in ***\n" {
		t.Errorf("step 0 log = %q", got)
	}
	if got := api.postedText(1); got != "ran in ***\n" {
		t.Errorf("step 1 log = %q", got)
	}
	for i, l := range api.logs {
		if l.seq != i {
			t.Errorf("chunk %d has seq %d", i, l.seq)
		}
	}
}

func TestExecute_JobSecretsReachStepEnvAndAreMasked(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	spec := jobSpec(runnerapi.StepSpec{Run: "one", Env: map[string]string{"TOKEN": "${{ secrets.TOKEN }}"}})
	spec.Secrets = map[string]string{"TOKEN": "cz-job-7"}
	e.Execute(context.Background(), spec)

	if got := api.postedText(0); got != "ran in ***\n" {
		t.Errorf("log = %q: the Job's own secret was not masked", got)
	}
	if !strings.Contains(stubFile(t, "0.env"), "export TOKEN='cz-job-7'\n") {
		t.Errorf("secret not in the step env file: %q", stubFile(t, "0.env"))
	}
}

func TestExecute_NoSecretValueReachesDockerArgv(t *testing.T) {
	calls := stubDocker(t)
	e := newExecutor(&fakeAPI{})
	const secret, multi = "hunter2-value", "line1\nit's line2"
	spec := jobSpec(runnerapi.StepSpec{Run: "echo ${{ secrets.TOKEN }}", Env: map[string]string{"TOKEN": "${{ secrets.TOKEN }}", "PEM": "${{ secrets.PEM }}"}})
	spec.Secrets = map[string]string{"TOKEN": secret, "PEM": multi}
	e.Execute(context.Background(), spec)

	for _, c := range calls() {
		for _, a := range c {
			if strings.Contains(a, secret) || strings.Contains(a, "line2") {
				t.Errorf("secret value in docker %s argv: %q", c[0], c)
			}
		}
	}
	if env := stubFile(t, "0.env"); !strings.Contains(env, "export TOKEN='"+secret+"'\n") || !strings.Contains(env, `export PEM='line1
it'\''s line2'`) {
		t.Errorf("env file = %q", env)
	}
	if got := stubFile(t, "0.sh"); got != "echo "+secret {
		t.Errorf("script = %q", got)
	}
}

func TestExecute_OutputIsPostedWhileTheStepIsStillRunning(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	e.HeartbeatEvery = time.Hour
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { e.Execute(ctx, jobSpec(runnerapi.StepSpec{Run: "STUBTAIL"})); close(done) }()

	deadline := time.Now().Add(10 * time.Second)
	for api.postedText(0) != "first line\n" {
		if time.Now().After(deadline) {
			t.Fatalf("nothing posted while the step ran; calls = %v", calls())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if api.finished {
		t.Error("the Job had finished before the check")
	}
	cancel()
	<-done
}

func TestExecute_LogCapReportedByTheServerStopsFurtherPosts(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{truncate: true}
	newExecutor(api).Execute(context.Background(), jobSpec(runnerapi.StepSpec{Run: "one"}, runnerapi.StepSpec{Run: "two"}))
	if len(api.logs) != 1 {
		t.Errorf("posts after truncation = %d, want only the first", len(api.logs))
	}
	if api.result != "success" {
		t.Errorf("a capped log changed the result: %q %q", api.result, api.message)
	}
}

func artifactTar(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, h := range []struct {
		name, body string
		typ        byte
		link       string
	}{
		{"dist/", "", tar.TypeDir, ""},
		{"dist/app.bin", "binary", tar.TypeReg, ""},
		{"dist/link", "", tar.TypeSymlink, "/etc/passwd"},
		{"../escape", "x", tar.TypeReg, ""},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: h.name, Typeflag: h.typ, Mode: 0o755, Size: int64(len(h.body)), Linkname: h.link}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write([]byte(h.body))
	}
	_ = tw.Close()
	return buf.Bytes()
}

func TestExecute_ArtifactsAreZippedAndUploadedBeforeTheJobFinishes(t *testing.T) {
	calls := stubDocker(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("STUB_DIR"), "artifact.tar"), artifactTar(t), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	spec := jobSpec(runnerapi.StepSpec{Run: "make"})
	spec.Artifacts = []runnerapi.ArtifactSpec{{Name: "dist", Path: "./dist"}}
	newExecutor(api).Execute(context.Background(), spec)

	if api.result != "success" {
		t.Fatalf("result = %q %q", api.result, api.message)
	}
	zr, err := zip.NewReader(bytes.NewReader(api.artifacts["dist"]), int64(len(api.artifacts["dist"])))
	if err != nil {
		t.Fatalf("uploaded archive: %v", err)
	}
	var names []string
	for _, f := range zr.File {
		names = append(names, f.Name)
	}
	if !slices.Equal(names, []string{"dist/", "dist/app.bin"}) {
		t.Errorf("zip entries = %q, want the dir and file only: no symlink, nothing outside", names)
	}
	var cp call
	for _, c := range calls() {
		if c[0] == "cp" && c[1] != "-" {
			cp = c
		}
	}
	if !slices.Equal(cp, call{"cp", "cz-job-7:/workspace/src/dist", "-"}) {
		t.Errorf("cp args = %q", cp)
	}
}

func TestExecute_AFailedArtifactFailsTheJobAndNamesIt(t *testing.T) {
	for name, tc := range map[string]struct {
		path    string
		artErr  error
		wantMsg string
	}{
		"missing path":   {path: "nothing", wantMsg: `artifact "out": nothing does not exist in the workspace`},
		"outside":        {path: "../etc", wantMsg: "inside the workspace"},
		"server refusal": {path: "x", artErr: errors.New("422 a Run can have at most 10 artifacts (ci.max_artifacts_per_run)"), wantMsg: "ci.max_artifacts_per_run"},
	} {
		t.Run(name, func(t *testing.T) {
			stubDocker(t) // without artifact.tar docker cp reports the path missing
			api := &fakeAPI{artErr: tc.artErr}
			spec := jobSpec(runnerapi.StepSpec{Run: "make"})
			spec.Artifacts = []runnerapi.ArtifactSpec{{Name: "out", Path: tc.path}}
			if tc.artErr != nil {
				if err := os.WriteFile(filepath.Join(os.Getenv("STUB_DIR"), "artifact.tar"), artifactTar(t), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			newExecutor(api).Execute(context.Background(), spec)
			if api.result != "failure" || !strings.Contains(api.message, tc.wantMsg) {
				t.Errorf("result = %q %q, want failure containing %q", api.result, api.message, tc.wantMsg)
			}
		})
	}
}

func TestExecute_ConfiguredMaskerStillAppliesWhenTheJobHasSecrets(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	e := newExecutor(api)
	e.Masker = logmask.New([]string{"cz-job-7"})
	spec := jobSpec(runnerapi.StepSpec{Run: "one"})
	spec.Secrets = map[string]string{"UNRELATED": "other-secret"}
	e.Execute(context.Background(), spec)

	if got := api.postedText(0); got != "ran in ***\n" {
		t.Errorf("log = %q: the runner's own mask values were dropped", got)
	}
}

func TestExecute_FirstFailureSurvivesALaterBadCondition(t *testing.T) {
	stubDocker(t)
	api := &fakeAPI{}
	newExecutor(api).Execute(context.Background(), jobSpec(
		runnerapi.StepSpec{Name: "build", Run: "STUBFAIL"},
		runnerapi.StepSpec{Name: "later", Run: "x", If: "1 +"},
	))
	if api.result != "failure" || !strings.Contains(api.message, "step 1 (build)") {
		t.Errorf("result = %q %q, want the first step's failure", api.result, api.message)
	}
}

func TestExecute_ArtifactNameInterpolatesTheMatrix(t *testing.T) {
	stubDocker(t)
	if err := os.WriteFile(filepath.Join(os.Getenv("STUB_DIR"), "artifact.tar"), artifactTar(t), 0o644); err != nil {
		t.Fatal(err)
	}
	api := &fakeAPI{}
	spec := jobSpec(runnerapi.StepSpec{Run: "make"})
	spec.Matrix = map[string]string{"arch": "arm64"}
	spec.Artifacts = []runnerapi.ArtifactSpec{{Name: "dist-${{ matrix.arch }}", Path: "./dist"}}
	newExecutor(api).Execute(context.Background(), spec)

	if _, ok := api.artifacts["dist-arm64"]; !ok || api.result != "success" {
		t.Errorf("artifacts = %v, result %q %q", api.artifacts, api.result, api.message)
	}
}
