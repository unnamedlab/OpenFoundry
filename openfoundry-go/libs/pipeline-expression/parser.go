package pipelineexpression

import (
	"fmt"
	"strconv"
	"strings"
)

// LiteralKind tags a [Literal] payload.
type LiteralKind int

const (
	// LitBool — boolean literal.
	LitBool LiteralKind = iota
	// LitInteger — i64 literal.
	LitInteger
	// LitDouble — f64 literal.
	LitDouble
	// LitString — string literal.
	LitString
	// LitNull — null literal.
	LitNull
)

// Literal is a parsed scalar literal.
type Literal struct {
	Kind   LiteralKind
	Bool   bool
	Int    int64
	Double float64
	Str    string
}

// LiteralBool builds a boolean literal.
func LiteralBool(v bool) Literal { return Literal{Kind: LitBool, Bool: v} }

// LiteralInteger builds an integer literal.
func LiteralInteger(v int64) Literal { return Literal{Kind: LitInteger, Int: v} }

// LiteralDouble builds a double literal.
func LiteralDouble(v float64) Literal { return Literal{Kind: LitDouble, Double: v} }

// LiteralString builds a string literal.
func LiteralString(v string) Literal { return Literal{Kind: LitString, Str: v} }

// LiteralNull builds a null literal.
func LiteralNull() Literal { return Literal{Kind: LitNull} }

// ExprKind tags an [Expr].
type ExprKind int

const (
	// ExprLit — scalar literal.
	ExprLit ExprKind = iota
	// ExprColumn — bare identifier referencing a column.
	ExprColumn
	// ExprCall — function call `name(args...)`.
	ExprCall
	// ExprUnary — unary operator application.
	ExprUnary
	// ExprBinary — binary operator application.
	ExprBinary
)

// Expr is a parsed expression node.
type Expr struct {
	Kind     ExprKind
	Lit      Literal
	Name     string
	Args     []Expr
	UnaryOp  UnaryOp
	BinaryOp BinaryOp
	Operand  *Expr
	Left     *Expr
	Right    *Expr
}

// UnaryOp enumerates the supported unary operators.
type UnaryOp int

const (
	// UnaryNeg — arithmetic negation `-x`.
	UnaryNeg UnaryOp = iota
	// UnaryNot — logical negation `not x`.
	UnaryNot
)

// BinaryOp enumerates the supported binary operators.
type BinaryOp int

const (
	// BinaryAdd — addition `a + b`.
	BinaryAdd BinaryOp = iota
	// BinarySub — subtraction `a - b`.
	BinarySub
	// BinaryMul — multiplication `a * b`.
	BinaryMul
	// BinaryDiv — division `a / b`.
	BinaryDiv
	// BinaryEq — equality `a = b`.
	BinaryEq
	// BinaryNotEq — inequality `a != b`.
	BinaryNotEq
	// BinaryLt — strict-less `a < b`.
	BinaryLt
	// BinaryLte — less-or-equal `a <= b`.
	BinaryLte
	// BinaryGt — strict-greater `a > b`.
	BinaryGt
	// BinaryGte — greater-or-equal `a >= b`.
	BinaryGte
	// BinaryAnd — logical conjunction.
	BinaryAnd
	// BinaryOr — logical disjunction.
	BinaryOr
)

// IsComparison reports whether the operator yields a Boolean by
// comparing its operands.
func (op BinaryOp) IsComparison() bool {
	switch op {
	case BinaryEq, BinaryNotEq, BinaryLt, BinaryLte, BinaryGt, BinaryGte:
		return true
	}
	return false
}

// IsLogical reports whether the operator is a Boolean connective.
func (op BinaryOp) IsLogical() bool {
	return op == BinaryAnd || op == BinaryOr
}

// String returns the Go-format Display for the operator (matches the
// Rust derive(Debug) shape — used in error messages).
func (op BinaryOp) String() string {
	switch op {
	case BinaryAdd:
		return "Add"
	case BinarySub:
		return "Sub"
	case BinaryMul:
		return "Mul"
	case BinaryDiv:
		return "Div"
	case BinaryEq:
		return "Eq"
	case BinaryNotEq:
		return "NotEq"
	case BinaryLt:
		return "Lt"
	case BinaryLte:
		return "Lte"
	case BinaryGt:
		return "Gt"
	case BinaryGte:
		return "Gte"
	case BinaryAnd:
		return "And"
	case BinaryOr:
		return "Or"
	}
	return ""
}

// ParseErrorKind tags a [ParseError].
type ParseErrorKind int

const (
	// ParseErrUnexpectedEof — input ended mid-expression.
	ParseErrUnexpectedEof ParseErrorKind = iota
	// ParseErrUnexpected — unexpected token at offset.
	ParseErrUnexpected
	// ParseErrInvalidNumber — numeric literal could not be parsed.
	ParseErrInvalidNumber
	// ParseErrUnterminatedString — string literal had no closing quote.
	ParseErrUnterminatedString
)

// ParseError is the failure mode for [ParseExpr]. Mirrors the Rust
// thiserror enum.
type ParseError struct {
	Kind     ParseErrorKind
	Found    string
	Expected string
	Offset   int
	Number   string
}

// Error implements [error].
func (e *ParseError) Error() string {
	switch e.Kind {
	case ParseErrUnexpectedEof:
		return "unexpected end of input"
	case ParseErrUnexpected:
		return fmt.Sprintf("unexpected token '%s' at offset %d, expected %s", e.Found, e.Offset, e.Expected)
	case ParseErrInvalidNumber:
		return fmt.Sprintf("invalid numeric literal '%s'", e.Number)
	case ParseErrUnterminatedString:
		return "unterminated string literal"
	}
	return "unknown parse error"
}

// tokenKind discriminates a [token].
type tokenKind int

const (
	tokIdent tokenKind = iota
	tokNumber
	tokString
	tokLParen
	tokRParen
	tokComma
	tokPlus
	tokMinus
	tokStar
	tokSlash
	tokEq
	tokNotEq
	tokLt
	tokLte
	tokGt
	tokGte
)

type token struct {
	kind   tokenKind
	text   string
	offset int
}

// goDebugFmt mirrors `format!("{:?}", token)` for error messages.
func (t token) goDebugFmt() string {
	switch t.kind {
	case tokIdent:
		return fmt.Sprintf("Ident(%q)", t.text)
	case tokNumber:
		return fmt.Sprintf("Number(%q)", t.text)
	case tokString:
		return fmt.Sprintf("String(%q)", t.text)
	case tokLParen:
		return "LParen"
	case tokRParen:
		return "RParen"
	case tokComma:
		return "Comma"
	case tokPlus:
		return "Plus"
	case tokMinus:
		return "Minus"
	case tokStar:
		return "Star"
	case tokSlash:
		return "Slash"
	case tokEq:
		return "Eq"
	case tokNotEq:
		return "NotEq"
	case tokLt:
		return "Lt"
	case tokLte:
		return "Lte"
	case tokGt:
		return "Gt"
	case tokGte:
		return "Gte"
	}
	return ""
}

type lexer struct {
	bytes []byte
	pos   int
}

func newLexer(input string) *lexer {
	return &lexer{bytes: []byte(input)}
}

func (l *lexer) peekByte() (byte, bool) {
	if l.pos >= len(l.bytes) {
		return 0, false
	}
	return l.bytes[l.pos], true
}

func (l *lexer) skipWS() {
	for {
		b, ok := l.peekByte()
		if !ok {
			return
		}
		if !isASCIIWhitespace(b) {
			return
		}
		l.pos++
	}
}

func isASCIIWhitespace(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return false
}

func (l *lexer) nextToken() (*token, error) {
	l.skipWS()
	start := l.pos
	b, ok := l.peekByte()
	if !ok {
		return nil, nil
	}
	switch {
	case b == '(':
		l.pos++
		return &token{kind: tokLParen, offset: start}, nil
	case b == ')':
		l.pos++
		return &token{kind: tokRParen, offset: start}, nil
	case b == ',':
		l.pos++
		return &token{kind: tokComma, offset: start}, nil
	case b == '+':
		l.pos++
		return &token{kind: tokPlus, offset: start}, nil
	case b == '-':
		l.pos++
		return &token{kind: tokMinus, offset: start}, nil
	case b == '*':
		l.pos++
		return &token{kind: tokStar, offset: start}, nil
	case b == '/':
		l.pos++
		return &token{kind: tokSlash, offset: start}, nil
	case b == '=':
		l.pos++
		return &token{kind: tokEq, offset: start}, nil
	case b == '!':
		l.pos++
		if next, hasNext := l.peekByte(); hasNext && next == '=' {
			l.pos++
			return &token{kind: tokNotEq, offset: start}, nil
		}
		return nil, &ParseError{
			Kind:     ParseErrUnexpected,
			Found:    "!",
			Expected: "!=",
			Offset:   start,
		}
	case b == '<':
		l.pos++
		if next, hasNext := l.peekByte(); hasNext && next == '=' {
			l.pos++
			return &token{kind: tokLte, offset: start}, nil
		}
		return &token{kind: tokLt, offset: start}, nil
	case b == '>':
		l.pos++
		if next, hasNext := l.peekByte(); hasNext && next == '=' {
			l.pos++
			return &token{kind: tokGte, offset: start}, nil
		}
		return &token{kind: tokGt, offset: start}, nil
	case b == '\'' || b == '"':
		quote := b
		l.pos++
		begin := l.pos
		for {
			c, hasC := l.peekByte()
			if !hasC {
				return nil, &ParseError{Kind: ParseErrUnterminatedString}
			}
			if c == quote {
				s := string(l.bytes[begin:l.pos])
				l.pos++
				return &token{kind: tokString, text: s, offset: start}, nil
			}
			l.pos++
		}
	case isASCIIDigit(b) || b == '.':
		begin := l.pos
		for {
			c, hasC := l.peekByte()
			if !hasC {
				break
			}
			if isASCIIDigit(c) || c == '.' {
				l.pos++
				continue
			}
			break
		}
		s := string(l.bytes[begin:l.pos])
		return &token{kind: tokNumber, text: s, offset: start}, nil
	case isASCIIAlpha(b) || b == '_':
		begin := l.pos
		for {
			c, hasC := l.peekByte()
			if !hasC {
				break
			}
			if isASCIIAlphanumeric(c) || c == '_' {
				l.pos++
				continue
			}
			break
		}
		s := string(l.bytes[begin:l.pos])
		return &token{kind: tokIdent, text: s, offset: start}, nil
	}
	l.pos++
	return nil, &ParseError{
		Kind:     ParseErrUnexpected,
		Found:    string(rune(b)),
		Expected: "expression token",
		Offset:   start,
	}
}

func isASCIIDigit(b byte) bool       { return b >= '0' && b <= '9' }
func isASCIIAlpha(b byte) bool       { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }
func isASCIIAlphanumeric(b byte) bool {
	return isASCIIAlpha(b) || isASCIIDigit(b)
}

type parser struct {
	tokens []token
	pos    int
}

func (p *parser) peek() *token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

func (p *parser) peekOffset() int {
	if p.pos >= len(p.tokens) {
		return 0
	}
	return p.tokens[p.pos].offset
}

func (p *parser) advance() *token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	t := &p.tokens[p.pos]
	p.pos++
	return t
}

func (p *parser) expect(kind tokenKind, label string) error {
	t := p.advance()
	if t == nil {
		return &ParseError{Kind: ParseErrUnexpectedEof}
	}
	if t.kind != kind {
		return &ParseError{
			Kind:     ParseErrUnexpected,
			Found:    t.goDebugFmt(),
			Expected: label,
			Offset:   p.peekOffset(),
		}
	}
	return nil
}

func (p *parser) parseExpr() (Expr, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (Expr, error) {
	left, err := p.parseAnd()
	if err != nil {
		return Expr{}, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != tokIdent || !strings.EqualFold(t.text, "or") {
			break
		}
		p.advance()
		right, err := p.parseAnd()
		if err != nil {
			return Expr{}, err
		}
		l, r := left, right
		left = Expr{Kind: ExprBinary, BinaryOp: BinaryOr, Left: &l, Right: &r}
	}
	return left, nil
}

func (p *parser) parseAnd() (Expr, error) {
	left, err := p.parseCmp()
	if err != nil {
		return Expr{}, err
	}
	for {
		t := p.peek()
		if t == nil || t.kind != tokIdent || !strings.EqualFold(t.text, "and") {
			break
		}
		p.advance()
		right, err := p.parseCmp()
		if err != nil {
			return Expr{}, err
		}
		l, r := left, right
		left = Expr{Kind: ExprBinary, BinaryOp: BinaryAnd, Left: &l, Right: &r}
	}
	return left, nil
}

func (p *parser) parseCmp() (Expr, error) {
	left, err := p.parseAdd()
	if err != nil {
		return Expr{}, err
	}
	t := p.peek()
	if t == nil {
		return left, nil
	}
	var op BinaryOp
	switch t.kind {
	case tokEq:
		op = BinaryEq
	case tokNotEq:
		op = BinaryNotEq
	case tokLt:
		op = BinaryLt
	case tokLte:
		op = BinaryLte
	case tokGt:
		op = BinaryGt
	case tokGte:
		op = BinaryGte
	default:
		return left, nil
	}
	p.advance()
	right, err := p.parseAdd()
	if err != nil {
		return Expr{}, err
	}
	l, r := left, right
	return Expr{Kind: ExprBinary, BinaryOp: op, Left: &l, Right: &r}, nil
}

func (p *parser) parseAdd() (Expr, error) {
	left, err := p.parseMul()
	if err != nil {
		return Expr{}, err
	}
	for {
		t := p.peek()
		if t == nil {
			break
		}
		var op BinaryOp
		switch t.kind {
		case tokPlus:
			op = BinaryAdd
		case tokMinus:
			op = BinarySub
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseMul()
		if err != nil {
			return Expr{}, err
		}
		l, r := left, right
		left = Expr{Kind: ExprBinary, BinaryOp: op, Left: &l, Right: &r}
	}
	return left, nil
}

func (p *parser) parseMul() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return Expr{}, err
	}
	for {
		t := p.peek()
		if t == nil {
			break
		}
		var op BinaryOp
		switch t.kind {
		case tokStar:
			op = BinaryMul
		case tokSlash:
			op = BinaryDiv
		default:
			return left, nil
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return Expr{}, err
		}
		l, r := left, right
		left = Expr{Kind: ExprBinary, BinaryOp: op, Left: &l, Right: &r}
	}
	return left, nil
}

func (p *parser) parseUnary() (Expr, error) {
	t := p.peek()
	if t != nil {
		if t.kind == tokMinus {
			p.advance()
			inner, err := p.parseUnary()
			if err != nil {
				return Expr{}, err
			}
			operand := inner
			return Expr{Kind: ExprUnary, UnaryOp: UnaryNeg, Operand: &operand}, nil
		}
		if t.kind == tokIdent && strings.EqualFold(t.text, "not") {
			p.advance()
			inner, err := p.parseUnary()
			if err != nil {
				return Expr{}, err
			}
			operand := inner
			return Expr{Kind: ExprUnary, UnaryOp: UnaryNot, Operand: &operand}, nil
		}
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (Expr, error) {
	offset := p.peekOffset()
	t := p.advance()
	if t == nil {
		return Expr{}, &ParseError{Kind: ParseErrUnexpectedEof}
	}
	switch t.kind {
	case tokLParen:
		inner, err := p.parseExpr()
		if err != nil {
			return Expr{}, err
		}
		if err := p.expect(tokRParen, ")"); err != nil {
			return Expr{}, err
		}
		return inner, nil
	case tokNumber:
		n := t.text
		if strings.Contains(n, ".") {
			v, err := strconv.ParseFloat(n, 64)
			if err != nil {
				return Expr{}, &ParseError{Kind: ParseErrInvalidNumber, Number: n}
			}
			return Expr{Kind: ExprLit, Lit: LiteralDouble(v)}, nil
		}
		v, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return Expr{}, &ParseError{Kind: ParseErrInvalidNumber, Number: n}
		}
		return Expr{Kind: ExprLit, Lit: LiteralInteger(v)}, nil
	case tokString:
		return Expr{Kind: ExprLit, Lit: LiteralString(t.text)}, nil
	case tokIdent:
		name := t.text
		if strings.EqualFold(name, "true") {
			return Expr{Kind: ExprLit, Lit: LiteralBool(true)}, nil
		}
		if strings.EqualFold(name, "false") {
			return Expr{Kind: ExprLit, Lit: LiteralBool(false)}, nil
		}
		if strings.EqualFold(name, "null") {
			return Expr{Kind: ExprLit, Lit: LiteralNull()}, nil
		}
		next := p.peek()
		if next != nil && next.kind == tokLParen {
			p.advance()
			args := []Expr{}
			peek := p.peek()
			if peek == nil || peek.kind != tokRParen {
				for {
					arg, err := p.parseExpr()
					if err != nil {
						return Expr{}, err
					}
					args = append(args, arg)
					nxt := p.peek()
					if nxt == nil || nxt.kind != tokComma {
						break
					}
					p.advance()
				}
			}
			if err := p.expect(tokRParen, ")"); err != nil {
				return Expr{}, err
			}
			return Expr{Kind: ExprCall, Name: name, Args: args}, nil
		}
		return Expr{Kind: ExprColumn, Name: name}, nil
	}
	return Expr{}, &ParseError{
		Kind:     ParseErrUnexpected,
		Found:    t.goDebugFmt(),
		Expected: "primary expression",
		Offset:   offset,
	}
}

// ParseExpr parses an expression string into an [Expr] tree.
func ParseExpr(input string) (Expr, error) {
	lex := newLexer(input)
	tokens := []token{}
	for {
		t, err := lex.nextToken()
		if err != nil {
			return Expr{}, err
		}
		if t == nil {
			break
		}
		tokens = append(tokens, *t)
	}
	p := &parser{tokens: tokens}
	expr, err := p.parseExpr()
	if err != nil {
		return Expr{}, err
	}
	if p.pos != len(p.tokens) {
		offset := p.peekOffset()
		var found string
		if t := p.peek(); t != nil {
			found = t.goDebugFmt()
		} else {
			found = "RParen"
		}
		return Expr{}, &ParseError{
			Kind:     ParseErrUnexpected,
			Found:    found,
			Expected: "end of expression",
			Offset:   offset,
		}
	}
	return expr, nil
}

// String renders [Expr] using the same shape as the Rust Display impl
// — parenthesised binaries `(left op right)`, function calls
// `name(arg1, arg2)` and unary `-x` / `not x`.
func (e Expr) String() string {
	var b strings.Builder
	e.writeTo(&b)
	return b.String()
}

func (e Expr) writeTo(b *strings.Builder) {
	switch e.Kind {
	case ExprLit:
		switch e.Lit.Kind {
		case LitBool:
			fmt.Fprintf(b, "%t", e.Lit.Bool)
		case LitInteger:
			fmt.Fprintf(b, "%d", e.Lit.Int)
		case LitDouble:
			b.WriteString(formatRustFloat(e.Lit.Double))
		case LitString:
			fmt.Fprintf(b, "'%s'", e.Lit.Str)
		case LitNull:
			b.WriteString("null")
		}
	case ExprColumn:
		b.WriteString(e.Name)
	case ExprCall:
		b.WriteString(e.Name)
		b.WriteByte('(')
		for i, arg := range e.Args {
			if i > 0 {
				b.WriteString(", ")
			}
			arg.writeTo(b)
		}
		b.WriteByte(')')
	case ExprUnary:
		switch e.UnaryOp {
		case UnaryNeg:
			b.WriteByte('-')
		case UnaryNot:
			b.WriteString("not ")
		}
		if e.Operand != nil {
			e.Operand.writeTo(b)
		}
	case ExprBinary:
		var sym string
		switch e.BinaryOp {
		case BinaryAdd:
			sym = "+"
		case BinarySub:
			sym = "-"
		case BinaryMul:
			sym = "*"
		case BinaryDiv:
			sym = "/"
		case BinaryEq:
			sym = "="
		case BinaryNotEq:
			sym = "!="
		case BinaryLt:
			sym = "<"
		case BinaryLte:
			sym = "<="
		case BinaryGt:
			sym = ">"
		case BinaryGte:
			sym = ">="
		case BinaryAnd:
			sym = "and"
		case BinaryOr:
			sym = "or"
		}
		b.WriteByte('(')
		if e.Left != nil {
			e.Left.writeTo(b)
		}
		fmt.Fprintf(b, " %s ", sym)
		if e.Right != nil {
			e.Right.writeTo(b)
		}
		b.WriteByte(')')
	}
}

// formatRustFloat renders f64 the same way Rust's `{}` formatter does
// — integral-valued floats keep no decimals (used by [Expr.String]).
func formatRustFloat(d float64) string {
	if d == float64(int64(d)) && !hasFractional(d) {
		return strconv.FormatFloat(d, 'f', -1, 64)
	}
	return strconv.FormatFloat(d, 'g', -1, 64)
}

func hasFractional(d float64) bool {
	return d-float64(int64(d)) != 0
}
