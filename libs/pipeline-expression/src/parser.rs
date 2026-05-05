//! Hand-rolled lexer + Pratt parser for the Pipeline Builder expression
//! mini-language. No external dep — keeps the build surface tight.
//!
//! Grammar (informal):
//!
//! ```text
//! expr     := orExpr
//! orExpr   := andExpr ("or" andExpr)*
//! andExpr  := cmpExpr ("and" cmpExpr)*
//! cmpExpr  := addExpr (("=" | "!=" | "<" | "<=" | ">" | ">=") addExpr)?
//! addExpr  := mulExpr (("+" | "-") mulExpr)*
//! mulExpr  := unary (("*" | "/") unary)*
//! unary    := ("-" | "not") unary | primary
//! primary  := literal | call | column | "(" expr ")"
//! call     := identifier "(" (expr ("," expr)*)? ")"
//! literal  := number | string | bool | null
//! column   := identifier
//! ```

use std::fmt;
use thiserror::Error;

#[derive(Debug, Clone, PartialEq)]
pub enum Literal {
    Bool(bool),
    Integer(i64),
    Double(f64),
    String(String),
    Null,
}

impl Eq for Literal {}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum Expr {
    Lit(Literal),
    Column(String),
    Call { name: String, args: Vec<Expr> },
    Unary { op: UnaryOp, operand: Box<Expr> },
    Binary { op: BinaryOp, left: Box<Expr>, right: Box<Expr> },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum UnaryOp {
    Neg,
    Not,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BinaryOp {
    Add,
    Sub,
    Mul,
    Div,
    Eq,
    NotEq,
    Lt,
    Lte,
    Gt,
    Gte,
    And,
    Or,
}

impl BinaryOp {
    pub fn is_comparison(self) -> bool {
        matches!(
            self,
            BinaryOp::Eq
                | BinaryOp::NotEq
                | BinaryOp::Lt
                | BinaryOp::Lte
                | BinaryOp::Gt
                | BinaryOp::Gte
        )
    }

    pub fn is_logical(self) -> bool {
        matches!(self, BinaryOp::And | BinaryOp::Or)
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ParseError {
    #[error("unexpected end of input")]
    UnexpectedEof,
    #[error("unexpected token '{found}' at offset {offset}, expected {expected}")]
    Unexpected {
        found: String,
        expected: String,
        offset: usize,
    },
    #[error("invalid numeric literal '{0}'")]
    InvalidNumber(String),
    #[error("unterminated string literal")]
    UnterminatedString,
}

#[derive(Debug, Clone, PartialEq)]
enum Token {
    Ident(String),
    Number(String),
    String(String),
    LParen,
    RParen,
    Comma,
    Plus,
    Minus,
    Star,
    Slash,
    Eq,
    NotEq,
    Lt,
    Lte,
    Gt,
    Gte,
}

#[derive(Debug)]
struct Lexer<'a> {
    bytes: &'a [u8],
    pos: usize,
}

impl<'a> Lexer<'a> {
    fn new(input: &'a str) -> Self {
        Self { bytes: input.as_bytes(), pos: 0 }
    }

    fn peek_byte(&self) -> Option<u8> {
        self.bytes.get(self.pos).copied()
    }

    fn advance(&mut self) -> Option<u8> {
        let b = self.peek_byte()?;
        self.pos += 1;
        Some(b)
    }

    fn skip_ws(&mut self) {
        while let Some(b) = self.peek_byte() {
            if b.is_ascii_whitespace() {
                self.pos += 1;
            } else {
                break;
            }
        }
    }

    fn next_token(&mut self) -> Result<Option<(Token, usize)>, ParseError> {
        self.skip_ws();
        let start = self.pos;
        let Some(b) = self.peek_byte() else {
            return Ok(None);
        };
        let tok = match b {
            b'(' => {
                self.pos += 1;
                Token::LParen
            }
            b')' => {
                self.pos += 1;
                Token::RParen
            }
            b',' => {
                self.pos += 1;
                Token::Comma
            }
            b'+' => {
                self.pos += 1;
                Token::Plus
            }
            b'-' => {
                self.pos += 1;
                Token::Minus
            }
            b'*' => {
                self.pos += 1;
                Token::Star
            }
            b'/' => {
                self.pos += 1;
                Token::Slash
            }
            b'=' => {
                self.pos += 1;
                Token::Eq
            }
            b'!' => {
                self.pos += 1;
                if self.peek_byte() == Some(b'=') {
                    self.pos += 1;
                    Token::NotEq
                } else {
                    return Err(ParseError::Unexpected {
                        found: "!".into(),
                        expected: "!=".into(),
                        offset: start,
                    });
                }
            }
            b'<' => {
                self.pos += 1;
                if self.peek_byte() == Some(b'=') {
                    self.pos += 1;
                    Token::Lte
                } else {
                    Token::Lt
                }
            }
            b'>' => {
                self.pos += 1;
                if self.peek_byte() == Some(b'=') {
                    self.pos += 1;
                    Token::Gte
                } else {
                    Token::Gt
                }
            }
            b'\'' | b'"' => {
                let quote = b;
                self.pos += 1;
                let begin = self.pos;
                while let Some(c) = self.peek_byte() {
                    if c == quote {
                        let s = std::str::from_utf8(&self.bytes[begin..self.pos])
                            .unwrap_or("")
                            .to_string();
                        self.pos += 1;
                        return Ok(Some((Token::String(s), start)));
                    }
                    self.pos += 1;
                }
                return Err(ParseError::UnterminatedString);
            }
            d if d.is_ascii_digit() || d == b'.' => {
                let begin = self.pos;
                while let Some(c) = self.peek_byte() {
                    if c.is_ascii_digit() || c == b'.' {
                        self.pos += 1;
                    } else {
                        break;
                    }
                }
                let s = std::str::from_utf8(&self.bytes[begin..self.pos])
                    .unwrap_or("")
                    .to_string();
                Token::Number(s)
            }
            c if c.is_ascii_alphabetic() || c == b'_' => {
                let begin = self.pos;
                while let Some(c) = self.peek_byte() {
                    if c.is_ascii_alphanumeric() || c == b'_' {
                        self.pos += 1;
                    } else {
                        break;
                    }
                }
                let s = std::str::from_utf8(&self.bytes[begin..self.pos])
                    .unwrap_or("")
                    .to_string();
                Token::Ident(s)
            }
            _ => {
                self.advance();
                return Err(ParseError::Unexpected {
                    found: format!("{}", b as char),
                    expected: "expression token".into(),
                    offset: start,
                });
            }
        };
        Ok(Some((tok, start)))
    }
}

#[derive(Debug)]
struct Parser {
    tokens: Vec<(Token, usize)>,
    pos: usize,
}

impl Parser {
    fn peek(&self) -> Option<&Token> {
        self.tokens.get(self.pos).map(|(t, _)| t)
    }

    fn peek_offset(&self) -> usize {
        self.tokens
            .get(self.pos)
            .map(|(_, o)| *o)
            .unwrap_or_default()
    }

    fn advance(&mut self) -> Option<&Token> {
        let t = self.tokens.get(self.pos).map(|(t, _)| t);
        self.pos += 1;
        t
    }

    fn expect(&mut self, expected: &Token, label: &str) -> Result<(), ParseError> {
        match self.advance() {
            Some(t) if t == expected => Ok(()),
            Some(t) => Err(ParseError::Unexpected {
                found: format!("{:?}", t),
                expected: label.to_string(),
                offset: self.peek_offset(),
            }),
            None => Err(ParseError::UnexpectedEof),
        }
    }

    fn parse_expr(&mut self) -> Result<Expr, ParseError> {
        self.parse_or()
    }

    fn parse_or(&mut self) -> Result<Expr, ParseError> {
        let mut left = self.parse_and()?;
        while matches!(self.peek(), Some(Token::Ident(s)) if s.eq_ignore_ascii_case("or")) {
            self.advance();
            let right = self.parse_and()?;
            left = Expr::Binary {
                op: BinaryOp::Or,
                left: Box::new(left),
                right: Box::new(right),
            };
        }
        Ok(left)
    }

    fn parse_and(&mut self) -> Result<Expr, ParseError> {
        let mut left = self.parse_cmp()?;
        while matches!(self.peek(), Some(Token::Ident(s)) if s.eq_ignore_ascii_case("and")) {
            self.advance();
            let right = self.parse_cmp()?;
            left = Expr::Binary {
                op: BinaryOp::And,
                left: Box::new(left),
                right: Box::new(right),
            };
        }
        Ok(left)
    }

    fn parse_cmp(&mut self) -> Result<Expr, ParseError> {
        let left = self.parse_add()?;
        let op = match self.peek() {
            Some(Token::Eq) => BinaryOp::Eq,
            Some(Token::NotEq) => BinaryOp::NotEq,
            Some(Token::Lt) => BinaryOp::Lt,
            Some(Token::Lte) => BinaryOp::Lte,
            Some(Token::Gt) => BinaryOp::Gt,
            Some(Token::Gte) => BinaryOp::Gte,
            _ => return Ok(left),
        };
        self.advance();
        let right = self.parse_add()?;
        Ok(Expr::Binary { op, left: Box::new(left), right: Box::new(right) })
    }

    fn parse_add(&mut self) -> Result<Expr, ParseError> {
        let mut left = self.parse_mul()?;
        loop {
            let op = match self.peek() {
                Some(Token::Plus) => BinaryOp::Add,
                Some(Token::Minus) => BinaryOp::Sub,
                _ => break,
            };
            self.advance();
            let right = self.parse_mul()?;
            left = Expr::Binary { op, left: Box::new(left), right: Box::new(right) };
        }
        Ok(left)
    }

    fn parse_mul(&mut self) -> Result<Expr, ParseError> {
        let mut left = self.parse_unary()?;
        loop {
            let op = match self.peek() {
                Some(Token::Star) => BinaryOp::Mul,
                Some(Token::Slash) => BinaryOp::Div,
                _ => break,
            };
            self.advance();
            let right = self.parse_unary()?;
            left = Expr::Binary { op, left: Box::new(left), right: Box::new(right) };
        }
        Ok(left)
    }

    fn parse_unary(&mut self) -> Result<Expr, ParseError> {
        match self.peek() {
            Some(Token::Minus) => {
                self.advance();
                let inner = self.parse_unary()?;
                Ok(Expr::Unary { op: UnaryOp::Neg, operand: Box::new(inner) })
            }
            Some(Token::Ident(s)) if s.eq_ignore_ascii_case("not") => {
                self.advance();
                let inner = self.parse_unary()?;
                Ok(Expr::Unary { op: UnaryOp::Not, operand: Box::new(inner) })
            }
            _ => self.parse_primary(),
        }
    }

    fn parse_primary(&mut self) -> Result<Expr, ParseError> {
        let offset = self.peek_offset();
        let token = self.advance().cloned().ok_or(ParseError::UnexpectedEof)?;
        match token {
            Token::LParen => {
                let inner = self.parse_expr()?;
                self.expect(&Token::RParen, ")")?;
                Ok(inner)
            }
            Token::Number(n) => {
                if n.contains('.') {
                    let v: f64 = n
                        .parse()
                        .map_err(|_| ParseError::InvalidNumber(n.clone()))?;
                    Ok(Expr::Lit(Literal::Double(v)))
                } else {
                    let v: i64 = n
                        .parse()
                        .map_err(|_| ParseError::InvalidNumber(n.clone()))?;
                    Ok(Expr::Lit(Literal::Integer(v)))
                }
            }
            Token::String(s) => Ok(Expr::Lit(Literal::String(s))),
            Token::Ident(name) => {
                if name.eq_ignore_ascii_case("true") {
                    return Ok(Expr::Lit(Literal::Bool(true)));
                }
                if name.eq_ignore_ascii_case("false") {
                    return Ok(Expr::Lit(Literal::Bool(false)));
                }
                if name.eq_ignore_ascii_case("null") {
                    return Ok(Expr::Lit(Literal::Null));
                }
                if matches!(self.peek(), Some(Token::LParen)) {
                    self.advance();
                    let mut args = Vec::new();
                    if !matches!(self.peek(), Some(Token::RParen)) {
                        loop {
                            args.push(self.parse_expr()?);
                            match self.peek() {
                                Some(Token::Comma) => {
                                    self.advance();
                                }
                                _ => break,
                            }
                        }
                    }
                    self.expect(&Token::RParen, ")")?;
                    Ok(Expr::Call { name, args })
                } else {
                    Ok(Expr::Column(name))
                }
            }
            other => Err(ParseError::Unexpected {
                found: format!("{:?}", other),
                expected: "primary expression".into(),
                offset,
            }),
        }
    }
}

pub fn parse_expr(input: &str) -> Result<Expr, ParseError> {
    let mut lexer = Lexer::new(input);
    let mut tokens = Vec::new();
    while let Some(tok) = lexer.next_token()? {
        tokens.push(tok);
    }
    let mut parser = Parser { tokens, pos: 0 };
    let expr = parser.parse_expr()?;
    if parser.pos != parser.tokens.len() {
        let offset = parser.peek_offset();
        return Err(ParseError::Unexpected {
            found: format!("{:?}", parser.peek().cloned().unwrap_or(Token::RParen)),
            expected: "end of expression".into(),
            offset,
        });
    }
    Ok(expr)
}

impl fmt::Display for Expr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Expr::Lit(Literal::Bool(b)) => write!(f, "{b}"),
            Expr::Lit(Literal::Integer(i)) => write!(f, "{i}"),
            Expr::Lit(Literal::Double(d)) => write!(f, "{d}"),
            Expr::Lit(Literal::String(s)) => write!(f, "'{s}'"),
            Expr::Lit(Literal::Null) => write!(f, "null"),
            Expr::Column(c) => write!(f, "{c}"),
            Expr::Call { name, args } => {
                write!(f, "{name}(")?;
                for (i, arg) in args.iter().enumerate() {
                    if i > 0 {
                        write!(f, ", ")?;
                    }
                    write!(f, "{arg}")?;
                }
                write!(f, ")")
            }
            Expr::Unary { op, operand } => {
                let sym = match op {
                    UnaryOp::Neg => "-",
                    UnaryOp::Not => "not ",
                };
                write!(f, "{sym}{operand}")
            }
            Expr::Binary { op, left, right } => {
                let sym = match op {
                    BinaryOp::Add => "+",
                    BinaryOp::Sub => "-",
                    BinaryOp::Mul => "*",
                    BinaryOp::Div => "/",
                    BinaryOp::Eq => "=",
                    BinaryOp::NotEq => "!=",
                    BinaryOp::Lt => "<",
                    BinaryOp::Lte => "<=",
                    BinaryOp::Gt => ">",
                    BinaryOp::Gte => ">=",
                    BinaryOp::And => "and",
                    BinaryOp::Or => "or",
                };
                write!(f, "({left} {sym} {right})")
            }
        }
    }
}
