package queryengine

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// literalKind enumerates the constant types our evaluator can fold to.
type literalKind int

const (
	kindInt literalKind = iota
	kindFloat
	kindBool
	kindString
	kindNull
)

// literalValue is an evaluated literal expression. Only the field
// matching `kind` carries a meaningful value.
type literalValue struct {
	kind      literalKind
	intVal    int64
	floatVal  float64
	boolVal   bool
	stringVal string
}

// expr is a tiny AST built by the parser. Numeric literals and
// arithmetic operators are folded to `literalValue` at eval time.
type expr struct {
	op       byte // 0 == leaf, otherwise '+', '-', '*', '/'
	left     *expr
	right    *expr
	leafTok  token
	leafKind literalKind // only valid when op == 0
}

// parseLiteralSelect parses sql and returns the expression list of a
// `SELECT <e1>, <e2>, …` statement. Anything else (FROM clauses,
// WHERE clauses, JOINs, …) yields ErrUnsupportedLocalExecution.
func parseLiteralSelect(sql string) ([]*expr, error) {
	tokens, err := tokenize(sql)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, ErrUnsupportedLocalExecution
	}

	p := &parser{tokens: tokens}
	if !p.acceptKeyword("select") {
		return nil, ErrUnsupportedLocalExecution
	}
	exprs := []*expr{}
	for {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, e)
		if !p.accept(tokComma) {
			break
		}
	}
	// Any trailing tokens (FROM, WHERE, …, even a trailing semicolon
	// followed by content) means we hit something we don't handle.
	if !p.atEnd() {
		// Allow a single trailing semicolon as a no-op terminator.
		if p.peek().kind == tokSemicolon {
			p.advance()
			if !p.atEnd() {
				return nil, ErrUnsupportedLocalExecution
			}
		} else {
			return nil, ErrUnsupportedLocalExecution
		}
	}
	return exprs, nil
}

// evalExpr folds an expr tree to a literal value. Arithmetic over
// mixed int/float promotes to float64 (matches DataFusion's coercion
// rules for the constant subset we handle). Strings, booleans and
// NULL only compose with themselves and never enter arithmetic.
func evalExpr(e *expr) (literalValue, error) {
	if e.op == 0 {
		return evalLeaf(e.leafTok, e.leafKind)
	}
	l, err := evalExpr(e.left)
	if err != nil {
		return literalValue{}, err
	}
	r, err := evalExpr(e.right)
	if err != nil {
		return literalValue{}, err
	}
	if l.kind == kindNull || r.kind == kindNull {
		return literalValue{kind: kindNull}, nil
	}
	if !isNumeric(l) || !isNumeric(r) {
		return literalValue{}, fmt.Errorf(
			"queryengine: %c requires numeric operands, got %v and %v: %w",
			e.op, l.kind, r.kind, ErrUnsupportedLocalExecution)
	}
	if l.kind == kindFloat || r.kind == kindFloat {
		x, y := toFloat(l), toFloat(r)
		var out float64
		switch e.op {
		case '+':
			out = x + y
		case '-':
			out = x - y
		case '*':
			out = x * y
		case '/':
			if y == 0 {
				return literalValue{}, fmt.Errorf("queryengine: division by zero")
			}
			out = x / y
		default:
			return literalValue{}, fmt.Errorf("queryengine: unknown op %c", e.op)
		}
		return literalValue{kind: kindFloat, floatVal: out}, nil
	}
	x, y := l.intVal, r.intVal
	switch e.op {
	case '+':
		return literalValue{kind: kindInt, intVal: x + y}, nil
	case '-':
		return literalValue{kind: kindInt, intVal: x - y}, nil
	case '*':
		return literalValue{kind: kindInt, intVal: x * y}, nil
	case '/':
		if y == 0 {
			return literalValue{}, fmt.Errorf("queryengine: division by zero")
		}
		return literalValue{kind: kindInt, intVal: x / y}, nil
	}
	return literalValue{}, fmt.Errorf("queryengine: unknown op %c", e.op)
}

func isNumeric(v literalValue) bool { return v.kind == kindInt || v.kind == kindFloat }

func toFloat(v literalValue) float64 {
	if v.kind == kindFloat {
		return v.floatVal
	}
	return float64(v.intVal)
}

func evalLeaf(t token, kind literalKind) (literalValue, error) {
	switch kind {
	case kindInt:
		n, err := strconv.ParseInt(t.text, 10, 64)
		if err != nil {
			return literalValue{}, fmt.Errorf("queryengine: invalid integer %q: %w", t.text, err)
		}
		return literalValue{kind: kindInt, intVal: n}, nil
	case kindFloat:
		f, err := strconv.ParseFloat(t.text, 64)
		if err != nil {
			return literalValue{}, fmt.Errorf("queryengine: invalid float %q: %w", t.text, err)
		}
		return literalValue{kind: kindFloat, floatVal: f}, nil
	case kindString:
		return literalValue{kind: kindString, stringVal: t.text}, nil
	case kindBool:
		return literalValue{kind: kindBool, boolVal: strings.EqualFold(t.text, "true")}, nil
	case kindNull:
		return literalValue{kind: kindNull}, nil
	}
	return literalValue{}, fmt.Errorf("queryengine: unknown leaf kind %d", kind)
}

// --- tokenizer ---------------------------------------------------------

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokNumberInt
	tokNumberFloat
	tokString
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokLParen
	tokRParen
	tokComma
	tokSemicolon
)

type token struct {
	kind tokenKind
	text string
}

func tokenize(sql string) ([]token, error) {
	var out []token
	runes := []rune(sql)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == ',':
			out = append(out, token{kind: tokComma})
			i++
		case r == ';':
			out = append(out, token{kind: tokSemicolon})
			i++
		case r == '(':
			out = append(out, token{kind: tokLParen})
			i++
		case r == ')':
			out = append(out, token{kind: tokRParen})
			i++
		case r == '+':
			out = append(out, token{kind: tokPlus})
			i++
		case r == '-':
			// Disambiguate unary minus inside an expression from the
			// binary operator: the parser handles unary minus by
			// folding it during expression building. Here we just
			// emit a `tokMinus` and let the parser sort it out.
			out = append(out, token{kind: tokMinus})
			i++
		case r == '*':
			out = append(out, token{kind: tokStar})
			i++
		case r == '/':
			out = append(out, token{kind: tokSlash})
			i++
		case r == '\'' || r == '"':
			s, j, err := readQuoted(runes, i, r)
			if err != nil {
				return nil, err
			}
			out = append(out, token{kind: tokString, text: s})
			i = j
		case unicode.IsDigit(r):
			s, isFloat, j, err := readNumber(runes, i)
			if err != nil {
				return nil, err
			}
			kind := tokNumberInt
			if isFloat {
				kind = tokNumberFloat
			}
			out = append(out, token{kind: kind, text: s})
			i = j
		case isIdentStart(r):
			s, j := readIdent(runes, i)
			out = append(out, token{kind: tokIdent, text: s})
			i = j
		default:
			return nil, fmt.Errorf("queryengine: unexpected character %q in SQL: %w",
				r, ErrUnsupportedLocalExecution)
		}
	}
	return out, nil
}

func readQuoted(runes []rune, start int, quote rune) (string, int, error) {
	if runes[start] != quote {
		return "", 0, fmt.Errorf("queryengine: internal: readQuoted called on %q", runes[start])
	}
	var b strings.Builder
	i := start + 1
	for i < len(runes) {
		c := runes[i]
		// SQL-style escape: '' or "" emits a single quote.
		if c == quote {
			if i+1 < len(runes) && runes[i+1] == quote {
				b.WriteRune(quote)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteRune(c)
		i++
	}
	return "", 0, fmt.Errorf("queryengine: unterminated string literal")
}

func readNumber(runes []rune, start int) (string, bool, int, error) {
	i := start
	isFloat := false
	for i < len(runes) {
		c := runes[i]
		switch {
		case unicode.IsDigit(c):
			i++
		case c == '.' && !isFloat:
			isFloat = true
			i++
		default:
			return string(runes[start:i]), isFloat, i, nil
		}
	}
	return string(runes[start:i]), isFloat, i, nil
}

func readIdent(runes []rune, start int) (string, int) {
	i := start
	for i < len(runes) && (isIdentStart(runes[i]) || unicode.IsDigit(runes[i])) {
		i++
	}
	return string(runes[start:i]), i
}

func isIdentStart(r rune) bool {
	return unicode.IsLetter(r) || r == '_'
}

// --- parser ------------------------------------------------------------

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) atEnd() bool        { return p.pos >= len(p.tokens) }
func (p *parser) peek() token        { return p.tokens[p.pos] }
func (p *parser) advance() token     { t := p.tokens[p.pos]; p.pos++; return t }
func (p *parser) accept(k tokenKind) bool {
	if p.atEnd() || p.tokens[p.pos].kind != k {
		return false
	}
	p.pos++
	return true
}

func (p *parser) acceptKeyword(kw string) bool {
	if p.atEnd() {
		return false
	}
	t := p.tokens[p.pos]
	if t.kind != tokIdent || !strings.EqualFold(t.text, kw) {
		return false
	}
	p.pos++
	return true
}

// parseExpr ::= addExpr
func (p *parser) parseExpr() (*expr, error) { return p.parseAdd() }

// addExpr ::= mulExpr ( ('+' | '-') mulExpr )*
func (p *parser) parseAdd() (*expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return nil, err
	}
	for !p.atEnd() {
		k := p.peek().kind
		if k != tokPlus && k != tokMinus {
			break
		}
		p.advance()
		right, err := p.parseMul()
		if err != nil {
			return nil, err
		}
		op := byte('+')
		if k == tokMinus {
			op = '-'
		}
		left = &expr{op: op, left: left, right: right}
	}
	return left, nil
}

// mulExpr ::= unary ( ('*' | '/') unary )*
func (p *parser) parseMul() (*expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for !p.atEnd() {
		k := p.peek().kind
		if k != tokStar && k != tokSlash {
			break
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		op := byte('*')
		if k == tokSlash {
			op = '/'
		}
		left = &expr{op: op, left: left, right: right}
	}
	return left, nil
}

// unary ::= ('+' | '-') unary | primary
func (p *parser) parseUnary() (*expr, error) {
	if p.accept(tokPlus) {
		return p.parseUnary()
	}
	if p.accept(tokMinus) {
		inner, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		// Fold: -<lit> becomes a literal with the sign embedded so
		// the leaf can be parsed by strconv directly.
		if inner.op == 0 {
			switch inner.leafKind {
			case kindInt, kindFloat:
				inner.leafTok.text = "-" + inner.leafTok.text
				return inner, nil
			}
		}
		// Otherwise treat unary minus as 0 - inner.
		zero := &expr{op: 0, leafKind: kindInt, leafTok: token{kind: tokNumberInt, text: "0"}}
		return &expr{op: '-', left: zero, right: inner}, nil
	}
	return p.parsePrimary()
}

// primary ::= number | string | bool-keyword | NULL | '(' expr ')'
func (p *parser) parsePrimary() (*expr, error) {
	if p.atEnd() {
		return nil, fmt.Errorf("queryengine: expression ended early: %w", ErrUnsupportedLocalExecution)
	}
	t := p.peek()
	switch t.kind {
	case tokNumberInt:
		p.advance()
		return &expr{op: 0, leafKind: kindInt, leafTok: t}, nil
	case tokNumberFloat:
		p.advance()
		return &expr{op: 0, leafKind: kindFloat, leafTok: t}, nil
	case tokString:
		p.advance()
		return &expr{op: 0, leafKind: kindString, leafTok: t}, nil
	case tokIdent:
		switch strings.ToLower(t.text) {
		case "true", "false":
			p.advance()
			return &expr{op: 0, leafKind: kindBool, leafTok: t}, nil
		case "null":
			p.advance()
			return &expr{op: 0, leafKind: kindNull, leafTok: t}, nil
		default:
			return nil, ErrUnsupportedLocalExecution
		}
	case tokLParen:
		p.advance()
		inner, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if !p.accept(tokRParen) {
			return nil, fmt.Errorf("queryengine: missing ')': %w", ErrUnsupportedLocalExecution)
		}
		return inner, nil
	}
	return nil, ErrUnsupportedLocalExecution
}
