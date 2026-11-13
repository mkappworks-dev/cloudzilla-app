package workflow

import (
	"strings"
	"testing"
)

var fake = Context{
	GitHub: map[string]string{"ref": "refs/heads/main", "sha": "abc", "event_name": "push", "run_number": "7", "actor": "Ann"},
	Env:    map[string]string{"MODE": "fast"},
	Matrix: map[string]string{"go": "1.23"},
	Needs:  map[string]string{"build": "success", "lint": "failure"},
}

func with(c Context, status string) Context {
	c.Status = status
	return c
}

func TestIfFromSpecExample(t *testing.T) {
	src := `
on: push
jobs:
  a:
    runs-on: x
    steps:
      - run: notify
        if: ${{ failure() && github.ref == 'refs/heads/main' }}
`
	w := mustParse(t, src)
	cond := w.Jobs[0].Steps[0].If
	for status, want := range map[string]bool{"failure": true, "success": false, "cancelled": false} {
		got, err := cond.Bool(with(fake, status))
		if err != nil || got != want {
			t.Errorf("status %s: got %v, %v want %v", status, got, err, want)
		}
	}
	other := with(fake, "failure")
	other.GitHub = map[string]string{"ref": "refs/heads/dev"}
	if got, _ := cond.Bool(other); got {
		t.Error("ref mismatch must be false")
	}
}

func TestUnknownFunctionAndContextFailAtParse(t *testing.T) {
	for src, line := range map[string]int{
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        if: nope()\n":             7,
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: echo ${{ toJSON(env) }}\n":           6,
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        if: vars.X == '1'\n":      7,
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        if: github.token != ''\n": 7,
		"on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        if: a < b\n":              7,
	} {
		e := firstError(t, src)
		if e.Line != line {
			t.Errorf("%q: line %d, want %d (%v)", src, e.Line, line, e)
		}
	}
	e := firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: x\n        if: nope()\n")
	if !strings.Contains(e.Message, `unknown function "nope"`) {
		t.Errorf("got %v", e)
	}
}

func TestEvalTable(t *testing.T) {
	tests := []struct {
		expr string
		want any
	}{
		{"github.ref == 'refs/heads/main'", true},
		{"github.ref != 'refs/heads/main'", false},
		{"'ABC' == 'abc'", true},
		{"1 == '1'", true},
		{"true && 'x'", "x"},
		{"'' || 'fallback'", "fallback"},
		{"!false", true},
		{"!(1 == 2 || 3 == 3)", false},
		{"null == ''", true},
		{"contains('Hello World', 'lo w')", true},
		{"contains(github.actor, 'zzz')", false},
		{"startsWith(github.ref, 'refs/heads/')", true},
		{"endsWith(github.ref, '/main')", true},
		{"format('{0}-{1} {{x}}', github.sha, 5)", "abc-5 {x}"},
		{"env.MODE == 'fast' && matrix.go == '1.23'", true},
		{"needs.build.result == 'success'", true},
		{"needs.lint.result == 'success'", false},
		{"matrix.missing == ''", true},
		{"'it''s'", "it's"},
		{"success()", true},
		{"failure()", false},
		{"always()", true},
		{"cancelled()", false},
		{"github.run_number == 7", true},
	}
	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			e, err := ParseExpr(tt.expr)
			if err != nil {
				t.Fatal(err)
			}
			got, err := e.Eval(fake)
			if err != nil || got != tt.want {
				t.Errorf("got %v (%v), want %v", got, err, tt.want)
			}
		})
	}
}

func TestConditionDefaultsToSuccess(t *testing.T) {
	var none *Expr
	if ok, _ := none.Bool(with(fake, "failure")); ok {
		t.Error("no if: must not run after a failure")
	}
	if ok, _ := none.Bool(fake); !ok {
		t.Error("no if: runs while successful")
	}
	e, _ := ParseExpr("github.event_name == 'push'")
	if ok, _ := e.Bool(with(fake, "failure")); ok {
		t.Error("an expression with no status function is wrapped in success()")
	}
	a, _ := ParseExpr("always() && github.event_name == 'push'")
	if ok, _ := a.Bool(with(fake, "cancelled")); !ok {
		t.Error("always() runs regardless")
	}
}

func TestRejectedExpressionConstructs(t *testing.T) {
	for _, src := range []string{
		"github.ref < 'a'", "github.ref >= 'a'", "github['ref']", "github.*", "env", "needs.build", "github.nope",
		"contains('a')", "format()", "success(1)", "toJSON(env)", "hashFiles('x')", "secrets", "'unterminated", "(1 == 1", "1 ==",
		"vars.X", "steps.a.outputs.b", "job.status", "runner.os",
	} {
		if _, err := ParseExpr(src); err == nil {
			t.Errorf("%q should be rejected", src)
		}
	}
}

func TestSecretsAreValidatedNotResolved(t *testing.T) {
	src := "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n      - run: deploy\n        env:\n          TOKEN: ${{ secrets.TOKEN }}\n"
	w := mustParse(t, src)
	got := w.Jobs[0].Steps[0].Env["TOKEN"]
	if got != "${{ secrets.TOKEN }}" {
		t.Errorf("secret reference must be left for the runner, got %q", got)
	}
	if _, err := Interpolate(got, fake); err == nil || !strings.Contains(err.Error(), "runner") {
		t.Errorf("server-side resolution must fail, got %v", err)
	}
}

func TestInterpolate(t *testing.T) {
	got, err := Interpolate("v${{ matrix.go }}-${{ github.sha }}${{ format('{0}', '}}') }}!", fake)
	if err != nil || got != "v1.23-abc}}!" {
		t.Errorf("got %q, %v", got, err)
	}
	if _, err := Interpolate("${{ github.sha", fake); err == nil {
		t.Error("unterminated must fail")
	}
}
