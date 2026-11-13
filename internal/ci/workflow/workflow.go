// Package workflow parses and validates Cloudzilla workflow files: a documented subset of GitHub Actions syntax.
package workflow

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Limits bound what one workflow may expand to.
type Limits struct {
	MaxJobs       int
	MaxSteps      int
	MaxJobMinutes int
}

var DefaultLimits = Limits{MaxJobs: 50, MaxSteps: 200, MaxJobMinutes: 360}

const defaultTimeoutMinutes = 60

// Error is one problem in a workflow file, positioned at the offending key or value.
type Error struct {
	File    string
	Line    int
	Column  int
	Message string
}

func (e Error) Error() string {
	return fmt.Sprintf("%s:%d:%d: %s", e.File, e.Line, e.Column, e.Message)
}

type Workflow struct {
	Name        string
	On          Triggers
	Env         map[string]string
	Concurrency *Concurrency
	// Jobs are concrete: a matrix job appears once per combination, in declaration order.
	Jobs []*Job
}

type Triggers struct {
	Push        *PushTrigger
	PullRequest *PullRequestTrigger
	Dispatch    bool
}

type PushTrigger struct{ Branches, Tags []string }

type PullRequestTrigger struct{ Types []string }

type Concurrency struct {
	Group            string
	CancelInProgress bool
}

type Job struct {
	// ID is the key under jobs; Name is the display name, with matrix values appended.
	ID             string
	Name           string
	RunsOn         []string
	Needs          []string
	If             *Expr
	Env            map[string]string
	Container      string
	TimeoutMinutes int
	FailFast       bool
	Matrix         map[string]string
	Steps          []Step
	Artifacts      []Artifact
}

type Step struct {
	Name             string
	Run              string
	Env              map[string]string
	If               *Expr
	TimeoutMinutes   int
	WorkingDirectory string
	Shell            string
}

type Artifact struct{ Name, Path string }

var rejectedKeys = map[string]bool{
	"uses": true, "services": true, "schedule": true, "paths": true, "paths-ignore": true,
	"pull_request_target": true, "permissions": true, "outputs": true, "environment": true,
	"defaults": true, "workflow_call": true, "inputs": true,
}

var yamlLine = regexp.MustCompile(`line (\d+)`)

// Parse parses a workflow with DefaultLimits. The Workflow is nil when there are errors.
func Parse(file string, data []byte) (*Workflow, []Error) {
	return ParseWithLimits(file, data, DefaultLimits)
}

func ParseWithLimits(file string, data []byte, lim Limits) (*Workflow, []Error) {
	d := &decoder{file: file, lim: lim}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		line := 1
		if m := yamlLine.FindStringSubmatch(err.Error()); m != nil {
			line, _ = strconv.Atoi(m[1])
		}
		return nil, []Error{{File: file, Line: line, Column: 1, Message: "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")}}
	}
	if len(doc.Content) == 0 {
		return nil, []Error{{File: file, Line: 1, Column: 1, Message: "workflow is empty"}}
	}
	w := d.workflow(doc.Content[0])
	if len(d.errs) > 0 {
		sort.SliceStable(d.errs, func(i, j int) bool {
			if d.errs[i].Line != d.errs[j].Line {
				return d.errs[i].Line < d.errs[j].Line
			}
			return d.errs[i].Column < d.errs[j].Column
		})
		return nil, d.errs
	}
	return w, nil
}

type decoder struct {
	file string
	lim  Limits
	errs []Error
}

func (d *decoder) errAt(n *yaml.Node, format string, a ...any) {
	d.errs = append(d.errs, Error{File: d.file, Line: n.Line, Column: n.Column, Message: fmt.Sprintf(format, a...)})
}

func deref(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.AliasNode {
		n = n.Alias
	}
	return n
}

func isNull(n *yaml.Node) bool { return n.Kind == yaml.ScalarNode && n.Tag == "!!null" }

// mapping walks a mapping node, dispatching known keys and reporting the rest. It returns the keys it saw.
func (d *decoder) mapping(n *yaml.Node, what string, fields map[string]func(k, v *yaml.Node)) map[string]*yaml.Node {
	seen := map[string]*yaml.Node{}
	n = deref(n)
	if n.Kind != yaml.MappingNode {
		d.errAt(n, "%s must be a mapping", what)
		return seen
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], deref(n.Content[i+1])
		if _, dup := seen[k.Value]; dup {
			d.errAt(k, "duplicate key %q", k.Value)
			continue
		}
		seen[k.Value] = k
		if f, ok := fields[k.Value]; ok {
			f(k, v)
		} else {
			d.unknownKey(k, k.Value)
		}
	}
	return seen
}

func (d *decoder) unknownKey(at *yaml.Node, key string) {
	if rejectedKeys[key] {
		d.errAt(at, "key %q is not supported", key)
		return
	}
	d.errAt(at, "unknown key %q", key)
}

// str returns a scalar's text, validating any ${{ }} in it.
func (d *decoder) str(n *yaml.Node, what string) string {
	n = deref(n)
	if n.Kind != yaml.ScalarNode {
		d.errAt(n, "%s must be a string", what)
		return ""
	}
	if isNull(n) {
		return ""
	}
	if _, err := splitTemplate(n.Value); err != nil {
		d.errAt(n, "%s: %v", what, err)
	}
	return n.Value
}

func (d *decoder) strList(n *yaml.Node, what string) []string {
	n = deref(n)
	if n.Kind == yaml.ScalarNode {
		return []string{d.str(n, what)}
	}
	if n.Kind != yaml.SequenceNode {
		d.errAt(n, "%s must be a string or a list of strings", what)
		return nil
	}
	var out []string
	for _, c := range n.Content {
		out = append(out, d.str(c, what))
	}
	return out
}

func (d *decoder) envMap(n *yaml.Node) map[string]string {
	out := map[string]string{}
	n = deref(n)
	if isNull(n) {
		return out
	}
	if n.Kind != yaml.MappingNode {
		d.errAt(n, "env must be a mapping")
		return out
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		out[n.Content[i].Value] = d.str(n.Content[i+1], "env value")
	}
	return out
}

func (d *decoder) cond(n *yaml.Node) *Expr {
	n = deref(n)
	if n.Kind != yaml.ScalarNode {
		d.errAt(n, "if must be an expression")
		return nil
	}
	e, err := ParseExpr(n.Value)
	if err != nil {
		d.errAt(n, "if: %v", err)
	}
	return e
}

func (d *decoder) minutes(n *yaml.Node) int {
	n = deref(n)
	v, err := strconv.Atoi(n.Value)
	if n.Kind != yaml.ScalarNode || n.Tag != "!!int" || err != nil || v <= 0 {
		d.errAt(n, "timeout-minutes must be a positive integer")
		return 0
	}
	if d.lim.MaxJobMinutes > 0 && v > d.lim.MaxJobMinutes {
		d.errAt(n, "timeout-minutes %d is over the limit of %d", v, d.lim.MaxJobMinutes)
	}
	return v
}

func (d *decoder) boolean(n *yaml.Node, what string) bool {
	n = deref(n)
	v, err := strconv.ParseBool(n.Value)
	if n.Kind != yaml.ScalarNode || n.Tag != "!!bool" || err != nil {
		d.errAt(n, "%s must be true or false", what)
	}
	return v
}

func (d *decoder) workflow(root *yaml.Node) *Workflow {
	root = deref(root)
	w := &Workflow{Env: map[string]string{}}
	var jobsNode *yaml.Node
	seen := d.mapping(root, "workflow", map[string]func(k, v *yaml.Node){
		"name":        func(_, v *yaml.Node) { w.Name = d.str(v, "name") },
		"on":          func(_, v *yaml.Node) { w.On = d.triggers(v) },
		"env":         func(_, v *yaml.Node) { w.Env = d.envMap(v) },
		"concurrency": func(_, v *yaml.Node) { w.Concurrency = d.concurrency(v) },
		"jobs":        func(_, v *yaml.Node) { jobsNode = v },
	})
	if root.Kind != yaml.MappingNode {
		return nil
	}
	if seen["on"] == nil {
		d.errAt(root, `"on" is required`)
	}
	if seen["jobs"] == nil {
		d.errAt(root, `"jobs" is required`)
	} else {
		w.Jobs = d.jobs(jobsNode)
	}
	if w.Name == "" {
		w.Name = path.Base(d.file)
	}
	return w
}

func (d *decoder) concurrency(n *yaml.Node) *Concurrency {
	n = deref(n)
	if n.Kind == yaml.ScalarNode {
		return &Concurrency{Group: d.str(n, "concurrency group")}
	}
	c := &Concurrency{}
	seen := d.mapping(n, "concurrency", map[string]func(k, v *yaml.Node){
		"group":              func(_, v *yaml.Node) { c.Group = d.str(v, "concurrency group") },
		"cancel-in-progress": func(_, v *yaml.Node) { c.CancelInProgress = d.boolean(v, "cancel-in-progress") },
	})
	if seen["group"] == nil && n.Kind == yaml.MappingNode {
		d.errAt(n, `concurrency requires "group"`)
	}
	return c
}

func (d *decoder) triggers(n *yaml.Node) Triggers {
	var t Triggers
	n = deref(n)
	add := func(at *yaml.Node, name string, v *yaml.Node) {
		switch name {
		case "push":
			t.Push = d.push(v)
		case "pull_request":
			t.PullRequest = d.pullRequest(v)
		case "workflow_dispatch":
			t.Dispatch = true
			if v != nil && !isNull(v) {
				d.mapping(v, "workflow_dispatch", nil)
			}
		default:
			d.unknownKey(at, name)
		}
	}
	switch n.Kind {
	case yaml.ScalarNode:
		add(n, n.Value, nil)
	case yaml.SequenceNode:
		for _, c := range n.Content {
			add(c, c.Value, nil)
		}
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			add(n.Content[i], n.Content[i].Value, deref(n.Content[i+1]))
		}
	default:
		d.errAt(n, `"on" must be an event name, a list or a mapping`)
	}
	return t
}

func (d *decoder) push(v *yaml.Node) *PushTrigger {
	p := &PushTrigger{}
	if v == nil || isNull(v) {
		return p
	}
	d.mapping(v, "push", map[string]func(k, v *yaml.Node){
		"branches": func(_, v *yaml.Node) { p.Branches = d.patterns(v, "branches") },
		"tags":     func(_, v *yaml.Node) { p.Tags = d.patterns(v, "tags") },
	})
	return p
}

func (d *decoder) patterns(n *yaml.Node, what string) []string {
	ps := d.strList(n, what)
	positive := false
	for _, p := range ps {
		positive = positive || !strings.HasPrefix(p, "!")
	}
	if len(ps) > 0 && !positive {
		d.errAt(n, "%s needs at least one pattern that does not start with !", what)
	}
	return ps
}

func (d *decoder) pullRequest(v *yaml.Node) *PullRequestTrigger {
	p := &PullRequestTrigger{Types: []string{"opened", "reopened", "synchronize"}}
	if v == nil || isNull(v) {
		return p
	}
	d.mapping(v, "pull_request", map[string]func(k, v *yaml.Node){
		"types": func(_, v *yaml.Node) {
			p.Types = nil
			for _, ty := range d.strList(v, "types") {
				if ty != "opened" && ty != "reopened" && ty != "synchronize" {
					d.errAt(deref(v), "pull_request type %q is not supported (use opened, reopened or synchronize)", ty)
				}
				p.Types = append(p.Types, ty)
			}
		},
	})
	return p
}

type rawJob struct {
	id      string
	key     *yaml.Node
	needsAt *yaml.Node
	needs   []string
	job     Job
	matrix  *matrixSpec
}

func (d *decoder) jobs(n *yaml.Node) []*Job {
	n = deref(n)
	if n.Kind != yaml.MappingNode || len(n.Content) == 0 {
		d.errAt(n, "jobs must be a mapping with at least one job")
		return nil
	}
	var raws []*rawJob
	byID := map[string]*rawJob{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i]
		if _, dup := byID[k.Value]; dup {
			d.errAt(k, "duplicate job id %q", k.Value)
			continue
		}
		r := d.job(k, deref(n.Content[i+1]))
		byID[k.Value] = r
		raws = append(raws, r)
	}
	d.graph(raws, byID)
	return d.expand(n, raws)
}

func (d *decoder) job(key, n *yaml.Node) *rawJob {
	r := &rawJob{id: key.Value, key: key}
	j := &r.job
	j.ID, j.FailFast, j.TimeoutMinutes = key.Value, true, defaultTimeoutMinutes
	before := len(d.errs)
	var stepsAt *yaml.Node
	seen := d.mapping(n, "job "+strconv.Quote(key.Value), map[string]func(k, v *yaml.Node){
		"name":            func(_, v *yaml.Node) { j.Name = d.str(v, "name") },
		"runs-on":         func(_, v *yaml.Node) { j.RunsOn = d.strList(v, "runs-on") },
		"needs":           func(_, v *yaml.Node) { r.needsAt = v; r.needs = d.strList(v, "needs") },
		"if":              func(_, v *yaml.Node) { j.If = d.cond(v) },
		"env":             func(_, v *yaml.Node) { j.Env = d.envMap(v) },
		"container":       func(_, v *yaml.Node) { j.Container = d.str(v, "container") },
		"timeout-minutes": func(_, v *yaml.Node) { j.TimeoutMinutes = d.minutes(v) },
		"strategy":        func(_, v *yaml.Node) { r.matrix = d.strategy(v, j) },
		"steps":           func(_, v *yaml.Node) { stepsAt = v; j.Steps = d.steps(v) },
		"artifacts":       func(_, v *yaml.Node) { j.Artifacts = d.artifacts(v) },
	})
	j.Needs = r.needs
	if len(d.errs) > before || n.Kind != yaml.MappingNode {
		return r
	}
	if seen["runs-on"] == nil {
		d.errAt(key, "job %q: runs-on is required", key.Value)
	}
	if stepsAt == nil {
		d.errAt(key, "job %q: steps must not be empty", key.Value)
	} else if len(j.Steps) == 0 {
		d.errAt(stepsAt, "job %q: steps must not be empty", key.Value)
	}
	return r
}

func (d *decoder) steps(n *yaml.Node) []Step {
	n = deref(n)
	if n.Kind != yaml.SequenceNode {
		d.errAt(n, "steps must be a list")
		return nil
	}
	var out []Step
	for _, c := range n.Content {
		c = deref(c)
		var s Step
		before := len(d.errs)
		seen := d.mapping(c, "step", map[string]func(k, v *yaml.Node){
			"name":              func(_, v *yaml.Node) { s.Name = d.str(v, "name") },
			"run":               func(_, v *yaml.Node) { s.Run = d.str(v, "run") },
			"env":               func(_, v *yaml.Node) { s.Env = d.envMap(v) },
			"if":                func(_, v *yaml.Node) { s.If = d.cond(v) },
			"timeout-minutes":   func(_, v *yaml.Node) { s.TimeoutMinutes = d.minutes(v) },
			"working-directory": func(_, v *yaml.Node) { s.WorkingDirectory = d.str(v, "working-directory") },
			"shell": func(_, v *yaml.Node) {
				s.Shell = d.str(v, "shell")
				if s.Shell != "sh" && s.Shell != "bash" {
					d.errAt(v, `shell must be "sh" or "bash"`)
				}
			},
		})
		if len(d.errs) == before && seen["run"] == nil {
			d.errAt(c, `step requires "run"`)
		}
		out = append(out, s)
	}
	return out
}

func (d *decoder) artifacts(n *yaml.Node) []Artifact {
	n = deref(n)
	if n.Kind != yaml.SequenceNode {
		d.errAt(n, "artifacts must be a list")
		return nil
	}
	var out []Artifact
	for _, c := range n.Content {
		c = deref(c)
		var a Artifact
		seen := d.mapping(c, "artifact", map[string]func(k, v *yaml.Node){
			"name": func(_, v *yaml.Node) { a.Name = d.str(v, "artifact name") },
			"path": func(_, v *yaml.Node) { a.Path = d.str(v, "artifact path") },
		})
		for _, req := range []string{"name", "path"} {
			if seen[req] == nil && c.Kind == yaml.MappingNode {
				d.errAt(c, "artifact requires %q", req)
			}
		}
		out = append(out, a)
	}
	return out
}

// graph reports unknown needs targets and cycles, naming the job ids.
func (d *decoder) graph(raws []*rawJob, byID map[string]*rawJob) {
	for _, r := range raws {
		for _, dep := range r.needs {
			if byID[dep] == nil {
				d.errAt(deref(r.needsAt), "job %q: needs unknown job %q", r.id, dep)
			}
		}
	}
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var stack []string
	var visit func(r *rawJob)
	visit = func(r *rawJob) {
		state[r.id] = visiting
		stack = append(stack, r.id)
		for _, dep := range r.needs {
			t := byID[dep]
			if t == nil {
				continue
			}
			switch state[dep] {
			case visiting:
				start := 0
				for stack[start] != dep {
					start++
				}
				cycle := append(append([]string{}, stack[start:]...), dep)
				d.errAt(byID[dep].key, "needs cycle: %s", strings.Join(cycle, " -> "))
			case 0:
				visit(t)
			}
		}
		stack = stack[:len(stack)-1]
		state[r.id] = done
	}
	for _, r := range raws {
		if state[r.id] == 0 {
			visit(r)
		}
	}
}
