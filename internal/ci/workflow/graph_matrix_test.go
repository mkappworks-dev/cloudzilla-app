package workflow

import (
	"fmt"
	"strings"
	"testing"
)

func job(id, extra string) string {
	return fmt.Sprintf("  %s:\n    runs-on: x\n%s    steps:\n      - run: y\n", id, extra)
}

func TestJobGraphErrors(t *testing.T) {
	tests := []struct {
		name string
		jobs string
		line int
		msg  []string
	}{
		{"unknown needs", job("a", "") + job("b", "    needs: [a, nope]\n"), 9, []string{`job "b"`, `unknown job "nope"`}},
		{"self cycle", job("a", "    needs: a\n"), 3, []string{"needs cycle", "a -> a"}},
		{"three cycle", job("a", "    needs: c\n") + job("b", "    needs: a\n") + job("c", "    needs: b\n"), 3, []string{"a -> c -> b -> a"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := firstError(t, "on: push\njobs:\n"+tt.jobs)
			if e.Line != tt.line {
				t.Errorf("line = %d, want %d (%v)", e.Line, tt.line, e)
			}
			for _, m := range tt.msg {
				if !strings.Contains(e.Message, m) {
					t.Errorf("message %q lacks %q", e.Message, m)
				}
			}
		})
	}
}

func matrixWorkflow(matrix string) string {
	return "on: push\njobs:\n  build:\n    runs-on: x\n    strategy:\n      matrix:\n" + matrix +
		"    steps:\n      - run: y\n"
}

func names(w *Workflow) string {
	var out []string
	for _, j := range w.Jobs {
		out = append(out, j.Name)
	}
	return strings.Join(out, "; ")
}

func TestMatrixExpansion(t *testing.T) {
	tests := []struct {
		name, matrix, want string
	}{
		{"axes", "        a: [1, 2]\n        b: [x, y]\n", "build (1, x); build (1, y); build (2, x); build (2, y)"},
		{"exclude", "        a: [1, 2]\n        b: [x, y]\n        exclude:\n          - a: 1\n            b: y\n", "build (1, x); build (2, x); build (2, y)"},
		{"include adds a new combination", "        a: [1]\n        include:\n          - a: 9\n            z: q\n", "build (1); build (9, q)"},
		{"include extends a match without renaming", "        a: [1, 2]\n        include:\n          - a: 2\n            extra: e\n", "build (1); build (2)"},
		{"include only", "        include:\n          - os: linux\n          - os: mac\n", "build (linux); build (mac)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := names(mustParse(t, matrixWorkflow(tt.matrix))); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMatrixValuesReachJobs(t *testing.T) {
	w := mustParse(t, matrixWorkflow("        a: [1, 2]\n        include:\n          - a: 2\n            extra: e\n"))
	if w.Jobs[1].Matrix["extra"] != "e" || w.Jobs[1].Matrix["a"] != "2" || w.Jobs[0].Matrix["extra"] != "" {
		t.Errorf("matrix = %v / %v", w.Jobs[0].Matrix, w.Jobs[1].Matrix)
	}
	if !w.Jobs[0].FailFast {
		t.Error("fail-fast defaults to true")
	}
}

func TestMatrixLimits(t *testing.T) {
	seven := "        a: [1,2,3,4,5,6,7]\n        b: [1,2,3,4,5,6,7,8]\n"
	e := firstError(t, matrixWorkflow(seven))
	if !strings.Contains(e.Message, "job limit exceeded") || !strings.Contains(e.Message, "56") || !strings.Contains(e.Message, "50") {
		t.Errorf("got %v", e)
	}
	e = firstError(t, matrixWorkflow(seven+"        c: [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30]\n"))
	if !strings.Contains(e.Message, "job limit exceeded") {
		t.Errorf("huge matrix: %v", e)
	}

	w, errs := ParseWithLimits("wf.yml", []byte(matrixWorkflow(seven)), Limits{MaxJobs: 56, MaxSteps: 200, MaxJobMinutes: 360})
	if len(errs) > 0 || len(w.Jobs) != 56 {
		t.Errorf("custom MaxJobs: %v", errs)
	}

	steps := strings.Repeat("      - run: y\n", 201)
	e = firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    steps:\n"+steps)
	if !strings.Contains(e.Message, "step limit exceeded") || !strings.Contains(e.Message, "201") {
		t.Errorf("steps: %v", e)
	}
	// steps count after expansion: 101 steps x 2 combinations
	e = firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    strategy:\n      matrix:\n        v: [1, 2]\n    steps:\n"+strings.Repeat("      - run: y\n", 101))
	if !strings.Contains(e.Message, "step limit exceeded") {
		t.Errorf("expanded steps: %v", e)
	}
}

func TestTimeoutOverMax(t *testing.T) {
	e := firstError(t, "on: push\njobs:\n  a:\n    runs-on: x\n    timeout-minutes: 361\n    steps:\n      - run: y\n")
	if e.Line != 5 || !strings.Contains(e.Message, "361") {
		t.Errorf("got %v", e)
	}
}
