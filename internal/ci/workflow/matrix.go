package workflow

import (
	"strings"

	"go.yaml.in/yaml/v3"
)

type pair struct{ k, v string }

type matrixSpec struct {
	axes    []string
	values  map[string][]string
	include [][]pair
	exclude [][]pair
}

type combo struct {
	vals []pair
	// shown is how many leading pairs appear in the job name; keys added by include stay out of it.
	shown int
}

func (d *decoder) strategy(n *yaml.Node, j *Job) *matrixSpec {
	var m *matrixSpec
	d.mapping(n, "strategy", map[string]func(k, v *yaml.Node){
		"fail-fast": func(_, v *yaml.Node) { j.FailFast = d.boolean(v, "fail-fast") },
		"matrix":    func(_, v *yaml.Node) { m = d.matrix(v) },
	})
	return m
}

func (d *decoder) matrix(n *yaml.Node) *matrixSpec {
	m := &matrixSpec{values: map[string][]string{}}
	n = deref(n)
	if n.Kind != yaml.MappingNode {
		d.errAt(n, "matrix must be a mapping")
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], deref(n.Content[i+1])
		switch k.Value {
		case "include":
			m.include = d.matrixEntries(v)
		case "exclude":
			m.exclude = d.matrixEntries(v)
		default:
			if v.Kind != yaml.SequenceNode || len(v.Content) == 0 {
				d.errAt(v, "matrix axis %q must be a non-empty list", k.Value)
				continue
			}
			m.axes = append(m.axes, k.Value)
			for _, c := range v.Content {
				m.values[k.Value] = append(m.values[k.Value], d.matrixScalar(c))
			}
		}
	}
	return m
}

func (d *decoder) matrixScalar(n *yaml.Node) string {
	n = deref(n)
	if n.Kind != yaml.ScalarNode {
		d.errAt(n, "matrix values must be strings, numbers or booleans")
		return ""
	}
	return n.Value
}

func (d *decoder) matrixEntries(n *yaml.Node) [][]pair {
	if n.Kind != yaml.SequenceNode {
		d.errAt(n, "include and exclude must be lists of mappings")
		return nil
	}
	var out [][]pair
	for _, c := range n.Content {
		c = deref(c)
		if c.Kind != yaml.MappingNode {
			d.errAt(c, "include and exclude entries must be mappings")
			continue
		}
		var e []pair
		for i := 0; i+1 < len(c.Content); i += 2 {
			e = append(e, pair{c.Content[i].Value, d.matrixScalar(c.Content[i+1])})
		}
		out = append(out, e)
	}
	return out
}

func lookup(ps []pair, k string) (string, bool) {
	for _, p := range ps {
		if p.k == k {
			return p.v, true
		}
	}
	return "", false
}

func subset(entry, vals []pair) bool {
	for _, e := range entry {
		if v, ok := lookup(vals, e.k); !ok || v != e.v {
			return false
		}
	}
	return true
}

func (m *matrixSpec) combos(maxCombos int) ([]combo, bool) {
	total := 1
	for _, a := range m.axes {
		total *= len(m.values[a])
		if total > maxCombos {
			return nil, false
		}
	}
	var out []combo
	if len(m.axes) > 0 {
		out = []combo{{}}
		for _, a := range m.axes {
			var next []combo
			for _, c := range out {
				for _, v := range m.values[a] {
					next = append(next, combo{vals: append(append([]pair{}, c.vals...), pair{a, v}), shown: len(c.vals) + 1})
				}
			}
			out = next
		}
	}
	kept := out[:0:0]
	for _, c := range out {
		excluded := false
		for _, e := range m.exclude {
			excluded = excluded || subset(e, c.vals)
		}
		if !excluded {
			kept = append(kept, c)
		}
	}
	out = kept
	base := len(out)
	for _, inc := range m.include {
		matched := false
		for i := 0; i < base && len(m.axes) > 0; i++ {
			if !out[i].includeMatches(inc, m.axes) {
				continue
			}
			matched = true
			for _, p := range inc {
				if _, ok := lookup(out[i].vals, p.k); !ok {
					out[i].vals = append(out[i].vals, p)
				}
			}
		}
		if !matched {
			out = append(out, combo{vals: append([]pair{}, inc...), shown: len(inc)})
		}
	}
	return out, true
}

// includeMatches compares only the original axes, as GitHub does, so an include can add keys to a combination.
func (c combo) includeMatches(inc []pair, axes []string) bool {
	for _, p := range inc {
		isAxis := false
		for _, a := range axes {
			isAxis = isAxis || a == p.k
		}
		if !isAxis {
			continue
		}
		if v, _ := lookup(c.vals, p.k); v != p.v {
			return false
		}
	}
	return true
}

// expand turns each raw job into one Job per matrix combination and enforces the job and step limits.
func (d *decoder) expand(jobsNode *yaml.Node, raws []*rawJob) []*Job {
	var out []*Job
	steps := 0
	seen := map[string]bool{}
	// workflow_jobs is UNIQUE (run_id, attempt, name), so a repeated name must fail here rather than at insert.
	unique := func(r *rawJob, name string) bool {
		if seen[name] {
			d.errAt(r.key, "job %q: duplicate job name %q", r.id, name)
			return false
		}
		seen[name] = true
		return true
	}
	for _, r := range raws {
		base := r.job.Name
		if base == "" {
			base = r.id
		}
		if r.matrix == nil {
			r.job.Name = base
			if !unique(r, base) {
				continue
			}
			out = append(out, jobPtr(r.job))
			steps += len(r.job.Steps)
			continue
		}
		cs, ok := r.matrix.combos(d.lim.MaxJobs*20 + 1000)
		if !ok {
			d.errAt(r.key, "job limit exceeded: matrix of job %q is far over the limit of %d jobs per run", r.id, d.lim.MaxJobs)
			return nil
		}
		if len(cs) == 0 {
			d.errAt(r.key, "job %q: matrix has no combinations left after exclude", r.id)
			continue
		}
		for _, c := range cs {
			j := r.job
			j.Matrix = map[string]string{}
			var names []string
			for i, p := range c.vals {
				j.Matrix[p.k] = p.v
				if i < c.shown {
					names = append(names, p.v)
				}
			}
			j.Name = base + " (" + strings.Join(names, ", ") + ")"
			if !unique(r, j.Name) {
				continue
			}
			out = append(out, jobPtr(j))
			steps += len(j.Steps)
		}
		if len(out) > d.lim.MaxJobs {
			d.errAt(r.key, "job limit exceeded: %d jobs after matrix expansion (job %q), the limit is %d per run", len(out), r.id, d.lim.MaxJobs)
			return nil
		}
	}
	if len(out) > d.lim.MaxJobs {
		d.errAt(jobsNode, "job limit exceeded: %d jobs, the limit is %d per run", len(out), d.lim.MaxJobs)
	}
	if steps > d.lim.MaxSteps {
		d.errAt(jobsNode, "step limit exceeded: %d steps after matrix expansion, the limit is %d per run", steps, d.lim.MaxSteps)
	}
	return out
}

func jobPtr(j Job) *Job { return &j }
