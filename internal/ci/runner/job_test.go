package runner

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/ci/runnerapi"
)

// stubScript stands in for the docker CLI: it logs each invocation's arguments (NUL separated, records ended by
// \001) and succeeds, except that a `run` whose arguments mention STUBFAIL exits 3 and one that mentions STUBHANG
// blocks until `rm -f` removes its container, as the real CLI would exit when its container is removed.
const stubScript = `#!/bin/sh
printf '%s\0' "$@" >> "$STUB_DIR/log"
printf '\001' >> "$STUB_DIR/log"
case "$1" in
  cp) cat > /dev/null ;;
  rm) for a in "$@"; do last=$a; done; : > "$STUB_DIR/removed-$last" ;;
  run)
    name=""; prev=""
    for a in "$@"; do [ "$prev" = "--name" ] && name=$a; prev=$a; done
    case "$*" in *STUBFAIL*) echo "boom" >&2; exit 3 ;; esac
    case "$*" in *STUBHANG*) while [ ! -e "$STUB_DIR/removed-$name" ]; do sleep 0.02; done; exit 137 ;; esac
    echo "ran $name" ;;
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

type fakeAPI struct {
	mu        sync.Mutex
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

func newExecutor(api *fakeAPI) *Executor {
	return &Executor{
		Config: &Config{Images: map[string]string{"linux": "ubuntu:24.04"}},
		Limits: DefaultLimits(), API: api, Out: &bytes.Buffer{}, HeartbeatEvery: 10 * time.Millisecond,
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
	starts := containerStarts(calls())
	if len(starts) != 3 {
		t.Fatalf("container starts = %d, want the init container and two steps: %v", len(starts), starts)
	}
	for _, c := range starts {
		for _, f := range wantHardening {
			if !c.has(f) {
				t.Errorf("%s %s lacks %s", c[0], c[2], f)
			}
		}
		for _, a := range c {
			if a == "--privileged" || strings.HasPrefix(a, "--cap-add") || strings.HasPrefix(a, "--device") || strings.HasPrefix(a, "--pid=") || strings.HasPrefix(a, "--pid ") ||
				strings.HasPrefix(a, "--ipc") || strings.HasPrefix(a, "--userns") || strings.Contains(a, "docker.sock") || a == "-v" ||
				(strings.HasPrefix(a, "--volume=") && !strings.HasPrefix(a, "--volume=cz-job-7:")) {
				t.Errorf("%s has a loosening argument %q", c[0], a)
			}
		}
	}
	first := starts[1]
	wantFirst := append(append([]string{"run", "--rm", "--name", "cz-job-7-step-0"}, wantHardening...),
		"--workdir=/workspace", "--entrypoint=sh",
		"--env=CI=true", "--env=CLOUDZILLA_ACTIONS=true", "--env=GITHUB_ACTOR=u", "--env=GITHUB_EVENT_NAME=push", "--env=GITHUB_REF=refs/heads/main",
		"--env=GITHUB_REF_NAME=main", "--env=GITHUB_REPOSITORY=o/r", "--env=GITHUB_RUN_NUMBER=1", "--env=GITHUB_SHA=abc",
		"--env=GITHUB_WORKSPACE=/workspace", "--env=GOFLAGS=-mod=mod",
		"--", "golang:1.23", "-e", "-c", "go build ./...")
	if !slices.Equal(first, wantFirst) {
		t.Errorf("step 0 args =\n%q\nwant\n%q", first, wantFirst)
	}
	second := starts[2]
	if !second.has("--workdir=/workspace/sub/dir") || !second.has("--entrypoint=bash") || !second.has("--env=A=1") || !second.has("--env=B=2") ||
		second[len(second)-1] != "echo abc" {
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
			for _, c := range containerStarts(calls()) {
				if c[0] == "run" {
					t.Errorf("a container started: %q", c)
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
	if starts := containerStarts(calls()); len(starts) != 2 || !starts[1].has("ubuntu:24.04") {
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
	want := []string{"rm -f cz-job-7-init", "volume rm -f cz-job-7", "network rm cz-job-7"}
	if !slices.Equal(tail, want) {
		t.Errorf("cleanup = %q, want %q", tail, want)
	}
}

func TestExecute_JobCancelledByTheServerStopsTheContainerAndReportsNothing(t *testing.T) {
	calls := stubDocker(t)
	api := &fakeAPI{}
	api.heartbeat = func() (bool, error) {
		return !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "run" }), nil
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
		if c[0] == "rm" && c.has("cz-job-7-step-0") {
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
		for !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "run" }) {
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
		killed = killed || (c[0] == "rm" && c.has("cz-job-7-step-0"))
	}
	if !killed {
		t.Error("container was not removed on stop")
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

func TestTarDir_EntriesAreWorldWritable(t *testing.T) {
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "sub", "a.txt"), []byte("a"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "run.sh"), []byte("#!/bin/sh"), 0o755)
	var buf bytes.Buffer
	if err := tarDir(dir, &buf); err != nil {
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
	want := map[string]int64{"./": 0o777, "sub/": 0o777, "sub/a.txt": 0o666, "run.sh": 0o777}
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
			if !slices.ContainsFunc(calls(), func(c call) bool { return c[0] == "rm" && c.has("cz-job-7-step-0") }) {
				t.Error("container was not removed")
			}
		})
	}
}
