package launcher

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// calculate evaluates an arithmetic expression such as "(1+2)*3^2 % 5". It
// takes + - * / % ^, the signs × and ÷, parentheses and decimals. ok is
// false for anything that is not a complete expression with at least one
// operator, so plain numbers and words are left to the other results.
func calculate(expr string) (value float64, ok bool) {
	expr = strings.NewReplacer("×", "*", "÷", "/", "（", "(", "）", ")", "，", "", ",", "").Replace(expr)
	if !strings.ContainsAny(expr, "+-*/%^") {
		return 0, false
	}
	p := &parser{text: expr}
	v, err := p.expression()
	p.skipSpace()
	if err != nil || p.pos != len(p.text) || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

// formatNumber prints a result without float noise: 0.1+0.2 shows as 0.3.
func formatNumber(v float64) string {
	s := strconv.FormatFloat(v, 'g', 12, 64)
	if strings.ContainsAny(s, "e") {
		return s
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

var errSyntax = errors.New("syntax")

// parser is a recursive-descent parser over the grammar
//
//	expression = term {("+" | "-") term}
//	term       = power {("*" | "/" | "%") power}
//	power      = unary ["^" power]
//	unary      = ["-" | "+"] unary | primary
//	primary    = number | "(" expression ")"
type parser struct {
	text string
	pos  int
}

func (p *parser) skipSpace() {
	for p.pos < len(p.text) && p.text[p.pos] == ' ' {
		p.pos++
	}
}

// accept consumes op if it comes next.
func (p *parser) accept(op byte) bool {
	p.skipSpace()
	if p.pos < len(p.text) && p.text[p.pos] == op {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expression() (float64, error) {
	v, err := p.term()
	for err == nil {
		switch {
		case p.accept('+'):
			var r float64
			r, err = p.term()
			v += r
		case p.accept('-'):
			var r float64
			r, err = p.term()
			v -= r
		default:
			return v, nil
		}
	}
	return 0, err
}

func (p *parser) term() (float64, error) {
	v, err := p.power()
	for err == nil {
		var op byte
		switch {
		case p.accept('*'):
			op = '*'
		case p.accept('/'):
			op = '/'
		case p.accept('%'):
			op = '%'
		default:
			return v, nil
		}
		var r float64
		if r, err = p.power(); err != nil {
			break
		}
		switch {
		case op == '*':
			v *= r
		case r == 0:
			err = errSyntax
		case op == '/':
			v /= r
		default:
			v = math.Mod(v, r)
		}
	}
	return 0, err
}

func (p *parser) power() (float64, error) {
	base, err := p.unary()
	if err != nil || !p.accept('^') {
		return base, err
	}
	exp, err := p.power()
	return math.Pow(base, exp), err
}

func (p *parser) unary() (float64, error) {
	switch {
	case p.accept('-'):
		v, err := p.unary()
		return -v, err
	case p.accept('+'):
		return p.unary()
	}
	return p.primary()
}

func (p *parser) primary() (float64, error) {
	if p.accept('(') {
		v, err := p.expression()
		if err != nil || !p.accept(')') {
			return 0, errSyntax
		}
		return v, nil
	}
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.text) && (p.text[p.pos] >= '0' && p.text[p.pos] <= '9' || p.text[p.pos] == '.') {
		p.pos++
	}
	if start == p.pos {
		return 0, errSyntax
	}
	return strconv.ParseFloat(p.text[start:p.pos], 64)
}
