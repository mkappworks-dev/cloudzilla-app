package workflow

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Workflow {
	t.Helper()
	w, errs := Parse("wf.yml", []byte(src))
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	return w
}

func firstError(t *testing.T, src string) Error {
	t.Helper()
	w, errs := Parse("wf.yml", []byte(src))
	if len(errs) == 0 {
		t.Fatalf("expected an error, got workflow %+v", w)
	}
	if w != nil {
		t.Fatalf("workflow must be nil when there are errors")
	}
	return errs[0]
}

const minimal = `
on: push
jobs:
  a:
    runs-on: linux
    steps:
      - run: echo hi
`

func TestParseMinimal(t *testing.T) {
	w := mustParse(t, minimal)
	if len(w.Jobs) != 1 || w.Jobs[0].ID != "a" || w.Jobs[0].Name != "a" {
		t.Fatalf("jobs = %+v", w.Jobs)
	}
	if w.Jobs[0].TimeoutMinutes != 60 {
		t.Errorf("default timeout = %d, want 60", w.Jobs[0].TimeoutMinutes)
	}
	if w.Jobs[0].Steps[0].Run != "echo hi" {
		t.Errorf("run = %q", w.Jobs[0].Steps[0].Run)
	}
	if w.Name != "wf.yml" {
		t.Errorf("default name = %q", w.Name)
	}
}

func TestParseRealWorldShellOnly(t *testing.T) {
	data, err := os.ReadFile("testdata/shell-only.yml")
	if err != nil {
		t.Fatal(err)
	}
	w, errs := Parse(".cloudzilla/workflows/ci.yml", data)
	if len(errs) > 0 {
		t.Fatalf("errors: %v", errs)
	}
	if w.Name != "CI" || w.Env["CGO_ENABLED"] != "0" {
		t.Errorf("name/env = %q %v", w.Name, w.Env)
	}
	if w.Concurrency == nil || w.Concurrency.Group != "ci-${{ github.ref }}" || !w.Concurrency.CancelInProgress {
		t.Errorf("concurrency = %+v", w.Concurrency)
	}
	var names []string
	for _, j := range w.Jobs {
		names = append(names, j.Name)
	}
	want := "lint, Test (1.22, linux), Test (1.23, linux), Test (1.23, darwin), deploy"
	if got := strings.Join(names, ", "); got != want {
		t.Errorf("job names\n got %s\nwant %s", got, want)
	}
	if got := w.Jobs[len(w.Jobs)-1].Artifacts; len(got) != 1 || got[0].Name != "dist" || got[0].Path != "dist/" {
		t.Errorf("artifacts = %+v", got)
	}
	if got := w.Jobs[0].Steps[1].Shell; got != "bash" {
		t.Errorf("shell = %q", got)
	}
}

func TestSupportedKeys(t *testing.T) {
	tests := []struct {
		name  string
		src   string
		check func(*Workflow) error
	}{
		{"name", "name: Build\n" + minimal, func(w *Workflow) error {
			return eq(w.Name, "Build")
		}},
		{"on scalar", minimal, func(w *Workflow) error {
			return eq(w.On.Push != nil, true)
		}},
		{"on list", "on: [push, pull_request]\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: y\n", func(w *Workflow) error {
			return eq(w.On.Push != nil && w.On.PullRequest != nil, true)
		}},
		{"on workflow_dispatch", "on:\n  workflow_dispatch:\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: y\n", func(w *Workflow) error {
			return eq(w.On.Dispatch, true)
		}},
		{"pull_request default types", "on: pull_request\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: y\n", func(w *Workflow) error {
			return eq(strings.Join(w.On.PullRequest.Types, ","), "opened,reopened,synchronize")
		}},
		{"env", "env:\n  A: 1\n  B: true\n" + minimal, func(w *Workflow) error {
			return eq(fmt.Sprint(w.Env), "map[A:1 B:true]")
		}},
		{"concurrency string", "concurrency: g\n" + minimal, func(w *Workflow) error {
			return eq(w.Concurrency.Group, "g")
		}},
		{"job name", strings.Replace(minimal, "    runs-on", "    name: Pretty\n    runs-on", 1), func(w *Workflow) error {
			return eq(w.Jobs[0].Name, "Pretty")
		}},
		{"runs-on list", strings.Replace(minimal, "runs-on: linux", "runs-on: [linux, gpu]", 1), func(w *Workflow) error {
			return eq(strings.Join(w.Jobs[0].RunsOn, ","), "linux,gpu")
		}},
		{"job env and container and timeout", strings.Replace(minimal, "    steps:", "    env: {K: v}\n    container: alpine:3\n    timeout-minutes: 5\n    steps:", 1), func(w *Workflow) error {
			j := w.Jobs[0]
			return eq(j.Env["K"]+j.Container+fmt.Sprint(j.TimeoutMinutes), "valpine:35")
		}},
		{"step keys", `
on: push
jobs:
  a:
    runs-on: x
    steps:
      - name: s
        run: r
        env: {E: "1"}
        if: always()
        timeout-minutes: 3
        working-directory: sub
        shell: sh
`, func(w *Workflow) error {
			s := w.Jobs[0].Steps[0]
			return eq(fmt.Sprintf("%s %v %v %d %s %s", s.Name, s.Env, s.If != nil, s.TimeoutMinutes, s.WorkingDirectory, s.Shell), "s map[E:1] true 3 sub sh")
		}},
		{"push filters", "on:\n  push:\n    branches: [main]\n    tags: ['v*']\n" + strings.TrimPrefix(minimal, "\non: push\n"), func(w *Workflow) error {
			return eq(fmt.Sprint(w.On.Push.Branches, w.On.Push.Tags), "[main] [v*]")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := mustParse(t, tt.src)
			if err := tt.check(w); err != nil {
				t.Error(err)
			}
		})
	}
}

func eq[T comparable](got, want T) error {
	if got != want {
		return fmt.Errorf("got %v, want %v", got, want)
	}
	return nil
}

func TestRejectedAndUnknownKeys(t *testing.T) {
	step := "      - run: x\n"
	tests := []struct {
		name string
		src  string
		line int
		msg  string
	}{
		{"step uses", "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - uses: actions/checkout@v4\n", 6, `key "uses" is not supported`},
		{"job uses (reusable workflow)", "on: push\njobs:\n  a:\n    uses: ./.cloudzilla/workflows/x.yml\n", 4, `key "uses" is not supported`},
		{"services", "on: push\njobs:\n  a:\n    runs-on: x\n    services:\n      db: {image: pg}\n    steps:\n" + step, 5, `key "services" is not supported`},
		{"schedule", "on:\n  schedule:\n    - cron: '* * * * *'\njobs: {}\n", 2, `key "schedule" is not supported`},
		{"paths", "on:\n  push:\n    paths: ['a']\njobs: {}\n", 3, `key "paths" is not supported`},
		{"pull_request_target", "on: pull_request_target\njobs: {}\n", 1, `key "pull_request_target" is not supported`},
		{"permissions", "permissions: read-all\n" + minimal, 1, `key "permissions" is not supported`},
		{"job permissions", "on: push\njobs:\n  a:\n    permissions: {}\n    runs-on: x\n    steps:\n" + step, 4, `key "permissions" is not supported`},
		{"outputs", "on: push\njobs:\n  a:\n    runs-on: x\n    outputs: {a: b}\n    steps:\n" + step, 5, `key "outputs" is not supported`},
		{"environment", "on: push\njobs:\n  a:\n    runs-on: x\n    environment: prod\n    steps:\n" + step, 5, `key "environment" is not supported`},
		{"defaults", "defaults:\n  run: {shell: bash}\n" + minimal, 1, `key "defaults" is not supported`},
		{"workflow_call", "on: workflow_call\njobs: {}\n", 1, `key "workflow_call" is not supported`},
		{"unknown top-level", "colour: red\n" + minimal, 1, `unknown key "colour"`},
		{"unknown job key", "on: push\njobs:\n  a:\n    runs-on: x\n    retries: 3\n    steps:\n" + step, 5, `unknown key "retries"`},
		{"unknown step key", "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        id: foo\n", 7, `unknown key "id"`},
		{"unknown event", "on: release\njobs: {}\n", 1, `unknown key "release"`},
		{"dispatch inputs", "on:\n  workflow_dispatch:\n    inputs: {}\njobs: {}\n", 3, `key "inputs" is not supported`},
		{"bad pull_request type", "on:\n  pull_request:\n    types: [closed]\njobs: {}\n", 3, `pull_request type "closed" is not supported`},
		{"bad shell", "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        shell: pwsh\n", 7, `shell must be "sh" or "bash"`},
		{"step without run", "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - name: only\n", 6, `step requires "run"`},
		{"missing runs-on", "on: push\njobs:\n  a:\n    steps:\n" + step, 3, `job "a": runs-on is required`},
		{"missing on", "jobs:\n  a:\n    runs-on: x\n    steps:\n" + step, 1, `"on" is required`},
		{"no jobs", "on: push\n", 1, `"jobs" is required`},
		{"timeout not number", "on: push\njobs:\n  a:\n    runs-on: x\n    timeout-minutes: soon\n    steps:\n" + step, 5, `timeout-minutes must be a positive integer`},
		{"artifact without path", "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n" + step + "    artifacts:\n      - name: n\n", 8, `artifact requires "path"`},
		{"yaml syntax", "on: [push\njobs: {}\n", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := firstError(t, tt.src)
			if tt.line != 0 && e.Line != tt.line {
				t.Errorf("line = %d, want %d (%v)", e.Line, tt.line, e)
			}
			if !strings.Contains(e.Message, tt.msg) {
				t.Errorf("message = %q, want it to contain %q", e.Message, tt.msg)
			}
			if e.File != "wf.yml" || e.Column < 1 {
				t.Errorf("file/column = %q/%d", e.File, e.Column)
			}
		})
	}
}

func TestEmptyStepsAndDuplicateJobs(t *testing.T) {
	e := firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    steps: []\n")
	if e.Line != 5 || !strings.Contains(e.Message, `job "a": steps must not be empty`) {
		t.Errorf("got %v", e)
	}
	e = firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n  a:\n    runs-on: x\n    steps:\n      - run: x\n")
	if e.Line != 7 || !strings.Contains(e.Message, `duplicate job id "a"`) {
		t.Errorf("got %v", e)
	}
}

func TestErrorString(t *testing.T) {
	e := Error{File: "f.yml", Line: 3, Column: 5, Message: "boom"}
	if e.Error() != "f.yml:3:5: boom" {
		t.Errorf("got %q", e.Error())
	}
}
