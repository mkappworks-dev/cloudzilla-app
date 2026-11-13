package workflow

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Context is what an expression sees. Secrets are deliberately absent: they are resolved on the runner only.
type Context struct {
	GitHub map[string]string
	Env    map[string]string
	Matrix map[string]string
	// Needs maps a job id to its result: success, failure, cancelled or skipped.
	Needs map[string]string
	// Status is the running status success(), failure() and cancelled() report; empty means success.
	Status string
}

var githubProps = map[string]bool{
	"sha": true, "ref": true, "ref_name": true, "event_name": true, "actor": true, "repository": true, "run_number": true,
}

// funcArity is {min, max}; max -1 is variadic.
var funcArity = map[string][2]int{
	"success": {0, 0}, "failure": {0, 0}, "always": {0, 0}, "cancelled": {0, 0},
	"contains": {2, 2}, "startswith": {2, 2}, "endswith": {2, 2}, "format": {1, -1},
}

var statusFuncs = map[string]bool{"success": true, "failure": true, "always": true, "cancelled": true}

// Expr is a parsed, validated expression.
type Expr struct {
	src       string
	root      node
	hasStatus bool
}

type node interface{}

type (
	litNode  struct{ v any }
	refNode  struct{ path []string }
	callNode struct {
		name string
		args []node
	}
	notNode struct{ x node }
	binNode struct {
		op   string
		l, r node
	}
)

// ParseExpr parses src, with or without a surrounding ${{ }}, and rejects unknown contexts and functions.
func ParseExpr(src string) (*Expr, error) {
	s := strings.TrimSpace(src)
	if strings.HasPrefix(s, "${{") && strings.HasSuffix(s, "}}") {
		s = s[3 : len(s)-2]
	}
	p := &exprParser{src: s}
	if err := p.lex(); err != nil {
		return nil, err
	}
	n, err := p.parseOr()
	if err != nil {
		return nil, err
	}
	if p.pos < len(p.toks) {
		return nil, fmt.Errorf("unexpected %q in expression", p.toks[p.pos].text)
	}
	return &Expr{src: strings.TrimSpace(s), root: n, hasStatus: p.hasStatus}, nil
}

func (e *Expr) String() string { return e.src }

type token struct {
	kind string // ident, num, str, op
	text string
}

type exprParser struct {
	src       string
	toks      []token
	pos       int
	hasStatus bool
}

func (p *exprParser) lex() error {
	s := p.src
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '\'':
			var b strings.Builder
			j := i + 1
			for {
				if j >= len(s) {
					return fmt.Errorf("unterminated string in expression")
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			p.toks = append(p.toks, token{"str", b.String()})
			i = j + 1
		case c >= '0' && c <= '9':
			j := i
			for j < len(s) && (s[j] >= '0' && s[j] <= '9' || s[j] == '.') {
				j++
			}
			p.toks = append(p.toks, token{"num", s[i:j]})
			i = j
		case isIdentStart(c):
			j := i
			for j < len(s) && (isIdentStart(s[j]) || s[j] >= '0' && s[j] <= '9' || s[j] == '-') {
				j++
			}
			p.toks = append(p.toks, token{"ident", s[i:j]})
			i = j
		case strings.HasPrefix(s[i:], "==") || strings.HasPrefix(s[i:], "!=") ||
			strings.HasPrefix(s[i:], "&&") || strings.HasPrefix(s[i:], "||"):
			p.toks = append(p.toks, token{"op", s[i : i+2]})
			i += 2
		case c == '!' || c == '(' || c == ')' || c == ',' || c == '.':
			p.toks = append(p.toks, token{"op", string(c)})
			i++
		default:
			return fmt.Errorf("unsupported character %q in expression", string(c))
		}
	}
	return nil
}

func isIdentStart(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func (p *exprParser) peek(op string) bool {
	return p.pos < len(p.toks) && p.toks[p.pos].kind == "op" && p.toks[p.pos].text == op
}

func (p *exprParser) parseOr() (node, error) {
	l, err := p.parseAnd()
	for err == nil && p.peek("||") {
		p.pos++
		var r node
		if r, err = p.parseAnd(); err == nil {
			l = binNode{"||", l, r}
		}
	}
	return l, err
}

func (p *exprParser) parseAnd() (node, error) {
	l, err := p.parseEq()
	for err == nil && p.peek("&&") {
		p.pos++
		var r node
		if r, err = p.parseEq(); err == nil {
			l = binNode{"&&", l, r}
		}
	}
	return l, err
}

func (p *exprParser) parseEq() (node, error) {
	l, err := p.parseUnary()
	for err == nil && (p.peek("==") || p.peek("!=")) {
		op := p.toks[p.pos].text
		p.pos++
		var r node
		if r, err = p.parseUnary(); err == nil {
			l = binNode{op, l, r}
		}
	}
	return l, err
}

func (p *exprParser) parseUnary() (node, error) {
	if p.peek("!") {
		p.pos++
		x, err := p.parseUnary()
		return notNode{x}, err
	}
	return p.parsePrimary()
}

func (p *exprParser) parsePrimary() (node, error) {
	if p.pos >= len(p.toks) {
		return nil, fmt.Errorf("unexpected end of expression")
	}
	t := p.toks[p.pos]
	p.pos++
	switch {
	case t.kind == "str":
		return litNode{t.text}, nil
	case t.kind == "num":
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number %q in expression", t.text)
		}
		return litNode{f}, nil
	case t.kind == "op" && t.text == "(":
		n, err := p.parseOr()
		if err != nil {
			return nil, err
		}
		if !p.peek(")") {
			return nil, fmt.Errorf("missing ) in expression")
		}
		p.pos++
		return n, nil
	case t.kind == "ident":
		return p.parseIdent(t.text)
	}
	return nil, fmt.Errorf("unexpected %q in expression", t.text)
}

func (p *exprParser) parseIdent(name string) (node, error) {
	switch name {
	case "true":
		return litNode{true}, nil
	case "false":
		return litNode{false}, nil
	case "null":
		return litNode{nil}, nil
	}
	if p.peek("(") {
		p.pos++
		lower := strings.ToLower(name)
		arity, ok := funcArity[lower]
		if !ok {
			return nil, fmt.Errorf("unknown function %q", name)
		}
		var args []node
		for !p.peek(")") {
			if len(args) > 0 {
				if !p.peek(",") {
					return nil, fmt.Errorf("expected , or ) in call to %s()", name)
				}
				p.pos++
			}
			a, err := p.parseOr()
			if err != nil {
				return nil, err
			}
			args = append(args, a)
		}
		p.pos++
		if len(args) < arity[0] || arity[1] >= 0 && len(args) > arity[1] {
			return nil, fmt.Errorf("wrong number of arguments to %s()", name)
		}
		if statusFuncs[lower] {
			p.hasStatus = true
		}
		return callNode{lower, args}, nil
	}
	path := []string{name}
	for p.peek(".") {
		p.pos++
		if p.pos >= len(p.toks) || p.toks[p.pos].kind != "ident" {
			return nil, fmt.Errorf("expected a name after . in expression")
		}
		path = append(path, p.toks[p.pos].text)
		p.pos++
	}
	return refNode{path}, validateRef(path)
}

func validateRef(path []string) error {
	root := path[0]
	switch root {
	case "github":
		if len(path) != 2 || !githubProps[path[1]] {
			return fmt.Errorf("unknown context %q", strings.Join(path, "."))
		}
	case "env", "secrets", "matrix":
		if len(path) != 2 {
			return fmt.Errorf("unknown context %q", strings.Join(path, "."))
		}
	case "needs":
		if len(path) != 3 || path[2] != "result" {
			return fmt.Errorf("unknown context %q: use needs.<job id>.result", strings.Join(path, "."))
		}
	default:
		return fmt.Errorf("unknown context %q", root)
	}
	return nil
}

// Eval evaluates the expression to a string, bool, float64 or nil.
func (e *Expr) Eval(ctx Context) (any, error) { return eval(e.root, ctx) }

// Bool evaluates e as a condition. A nil Expr is success(), and an expression with no status function is wrapped as
// success() && expr, as GitHub does.
func (e *Expr) Bool(ctx Context) (bool, error) {
	ok := ctx.Status == "" || ctx.Status == "success"
	if e == nil {
		return ok, nil
	}
	v, err := e.Eval(ctx)
	if err != nil {
		return false, err
	}
	if !e.hasStatus && !ok {
		return false, nil
	}
	return truthy(v), nil
}

func eval(n node, ctx Context) (any, error) {
	switch n := n.(type) {
	case litNode:
		return n.v, nil
	case refNode:
		return evalRef(n.path, ctx)
	case notNode:
		v, err := eval(n.x, ctx)
		return !truthy(v), err
	case binNode:
		l, err := eval(n.l, ctx)
		if err != nil {
			return nil, err
		}
		switch n.op {
		case "&&":
			if !truthy(l) {
				return l, nil
			}
			return eval(n.r, ctx)
		case "||":
			if truthy(l) {
				return l, nil
			}
			return eval(n.r, ctx)
		}
		r, err := eval(n.r, ctx)
		if err != nil {
			return nil, err
		}
		return looseEqual(l, r) == (n.op == "=="), nil
	case callNode:
		return evalCall(n, ctx)
	}
	return nil, fmt.Errorf("invalid expression")
}

func evalRef(path []string, ctx Context) (any, error) {
	switch path[0] {
	case "github":
		return ctx.GitHub[path[1]], nil
	case "env":
		return ctx.Env[path[1]], nil
	case "matrix":
		return ctx.Matrix[path[1]], nil
	case "needs":
		return ctx.Needs[path[1]], nil
	}
	return nil, fmt.Errorf("secrets.%s is only available on the runner", path[1])
}

func evalCall(c callNode, ctx Context) (any, error) {
	switch c.name {
	case "success":
		return ctx.Status == "" || ctx.Status == "success", nil
	case "failure":
		return ctx.Status == "failure", nil
	case "cancelled":
		return ctx.Status == "cancelled", nil
	case "always":
		return true, nil
	}
	args := make([]any, len(c.args))
	for i, a := range c.args {
		v, err := eval(a, ctx)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	switch c.name {
	case "contains":
		return strings.Contains(strings.ToLower(toStr(args[0])), strings.ToLower(toStr(args[1]))), nil
	case "startswith":
		return strings.HasPrefix(strings.ToLower(toStr(args[0])), strings.ToLower(toStr(args[1]))), nil
	case "endswith":
		return strings.HasSuffix(strings.ToLower(toStr(args[0])), strings.ToLower(toStr(args[1]))), nil
	}
	return formatArgs(toStr(args[0]), args[1:])
}

func formatArgs(f string, args []any) (string, error) {
	var b strings.Builder
	for i := 0; i < len(f); i++ {
		switch {
		case strings.HasPrefix(f[i:], "{{"):
			b.WriteByte('{')
			i++
		case strings.HasPrefix(f[i:], "}}"):
			b.WriteByte('}')
			i++
		case f[i] == '{':
			end := strings.IndexByte(f[i:], '}')
			if end < 0 {
				return "", fmt.Errorf("format(): unterminated {")
			}
			idx, err := strconv.Atoi(f[i+1 : i+end])
			if err != nil || idx < 0 || idx >= len(args) {
				return "", fmt.Errorf("format(): invalid placeholder %s", f[i:i+end+1])
			}
			b.WriteString(toStr(args[idx]))
			i += end
		default:
			b.WriteByte(f[i])
		}
	}
	return b.String(), nil
}

func truthy(v any) bool {
	switch v := v.(type) {
	case nil:
		return false
	case bool:
		return v
	case string:
		return v != ""
	case float64:
		return v != 0 && !math.IsNaN(v)
	}
	return true
}

func toStr(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

func toNum(v any) float64 {
	switch v := v.(type) {
	case nil:
		return 0
	case bool:
		if v {
			return 1
		}
		return 0
	case float64:
		return v
	case string:
		if strings.TrimSpace(v) == "" {
			return 0
		}
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return math.NaN()
		}
		return f
	}
	return math.NaN()
}

func looseEqual(a, b any) bool {
	as, aok := a.(string)
	bs, bok := b.(string)
	if aok && bok {
		return strings.EqualFold(as, bs)
	}
	if a == nil && b == nil {
		return true
	}
	return toNum(a) == toNum(b)
}

type segment struct {
	lit  string
	expr *Expr
}

// splitTemplate splits s on ${{ }} and parses each expression.
func splitTemplate(s string) ([]segment, error) {
	var segs []segment
	for {
		i := strings.Index(s, "${{")
		if i < 0 {
			break
		}
		end := closingBraces(s[i+3:])
		if end < 0 {
			return nil, fmt.Errorf("unterminated ${{ in %q", s)
		}
		e, err := ParseExpr(s[i+3 : i+3+end])
		if err != nil {
			return nil, err
		}
		if i > 0 {
			segs = append(segs, segment{lit: s[:i]})
		}
		segs = append(segs, segment{expr: e})
		s = s[i+3+end+2:]
	}
	if s != "" {
		segs = append(segs, segment{lit: s})
	}
	return segs, nil
}

// closingBraces finds }} outside a quoted string.
func closingBraces(s string) int {
	inStr := false
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\'':
			inStr = !inStr
		case !inStr && strings.HasPrefix(s[i:], "}}"):
			return i
		}
	}
	return -1
}

// Interpolate replaces every ${{ }} in s. A secrets.* reference is an error here: only the runner resolves it.
func Interpolate(s string, ctx Context) (string, error) {
	segs, err := splitTemplate(s)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, sg := range segs {
		if sg.expr == nil {
			b.WriteString(sg.lit)
			continue
		}
		v, err := sg.expr.Eval(ctx)
		if err != nil {
			return "", err
		}
		b.WriteString(toStr(v))
	}
	return b.String(), nil
}
