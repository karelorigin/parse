package css // import "github.com/tdewolff/parse/css"

import (
	"bytes"
	"io"
	"strconv"
)

////////////////////////////////////////////////////////////////

// GrammarType determines the type of grammar.
type GrammarType uint32

// GrammarType values
const (
	ErrorGrammar GrammarType = iota // extra token when errors occur
	AtRuleGrammar
	EndAtRuleGrammar
	RulesetGrammar
	EndRulesetGrammar
	DeclarationGrammar
	TokenGrammar
)

// String returns the string representation of a GrammarType.
func (tt GrammarType) String() string {
	switch tt {
	case ErrorGrammar:
		return "Error"
	case AtRuleGrammar:
		return "AtRule"
	case EndAtRuleGrammar:
		return "EndAtRule"
	case RulesetGrammar:
		return "Ruleset"
	case EndRulesetGrammar:
		return "EndRuleset"
	case DeclarationGrammar:
		return "Declaration"
	case TokenGrammar:
		return "Token"
	}
	return "Invalid(" + strconv.Itoa(int(tt)) + ")"
}

// ParserState denotes the state of the parser.
type ParserState uint32

// ParserState values
const (
	StylesheetState ParserState = iota
	AtRuleState
	RulesetState
)

////////////////////////////////////////////////////////////////

type TokenStream interface {
	Next() (TokenType, []byte)
	CopyFunc(func())
	Err() error
}

type Token struct {
	TokenType
	Data []byte
}

////////////////////////////////////////////////////////////////

type Parser2 struct {
	z     TokenStream
	state []ParserState

	buf []*TokenNode
	pos int
}

func NewParser2(z TokenStream) *Parser2 {
	p := &Parser2{
		z,
		[]ParserState{StylesheetState},
		make([]*TokenNode, 0, 16),
		0,
	}
	z.CopyFunc(p.copy)
	return p
}

func (p *Parser2) Parse() (*StylesheetNode, error) {
	var err error
	stylesheet := NewStylesheet()
	for {
		gt, n := p.Next()
		if gt == ErrorGrammar {
			err = p.z.Err()
			break
		}
		stylesheet.Nodes = append(stylesheet.Nodes, n)
		if err = p.parseRecursively(gt, n); err != nil {
			break
		}
	}
	if err != io.EOF {
		return stylesheet, err
	}
	return stylesheet, nil
}

func (p *Parser2) parseRecursively(rootGt GrammarType, n Node) error {
	if rootGt == AtRuleGrammar {
		atRule := n.(*AtRuleNode)
		for {
			gt, m := p.Next()
			if gt == ErrorGrammar {
				return p.z.Err()
			} else if gt == EndAtRuleGrammar {
				break
			}
			atRule.Rules = append(atRule.Rules, m)
			if err := p.parseRecursively(gt, m); err != nil {
				return err
			}
		}
	} else if rootGt == RulesetGrammar {
		ruleset := n.(*RulesetNode)
		for {
			gt, m := p.Next()
			if gt == ErrorGrammar {
				return p.z.Err()
			} else if gt == EndRulesetGrammar {
				break
			}
			if decl, ok := m.(*DeclarationNode); ok {
				ruleset.Decls = append(ruleset.Decls, decl)
			}
		}
	}
	return nil
}

// Err returns the error encountered during tokenization, this is often io.EOF but also other errors can be returned.
func (p Parser2) Err() error {
	return p.z.Err()
}

func (p *Parser2) Next() (GrammarType, Node) {
	if p.at(ErrorToken) {
		return ErrorGrammar, nil
	}
	p.skipWhitespace()

	// return End types
	state := p.State()
	if p.at(RightBraceToken) && (state == AtRuleState || state == RulesetState) || p.at(SemicolonToken) && state == AtRuleState {
		n := p.shift()
		p.skipWhile(SemicolonToken)

		p.state = p.state[:len(p.state)-1]
		if state == AtRuleState {

			return EndAtRuleGrammar, n
		}
		return EndRulesetGrammar, n
	}

	if p.at(CDOToken) || p.at(CDCToken) {
		return TokenGrammar, p.shift()
	} else if cn := p.parseAtRule(); cn != nil {
		return AtRuleGrammar, cn
	} else if cn := p.parseRuleset(); cn != nil {
		return RulesetGrammar, cn
	} else if cn := p.parseDeclaration(); cn != nil {
		return DeclarationGrammar, cn
	}
	return TokenGrammar, p.shift()
}

func (p *Parser2) State() ParserState {
	return p.state[len(p.state)-1]
}

func (p *Parser2) parseAtRule() *AtRuleNode {
	if !p.at(AtKeywordToken) {
		return nil
	}
	n := NewAtRule(p.shift())
	p.skipWhitespace()
	for !p.at(SemicolonToken) && !p.at(LeftBraceToken) && !p.at(ErrorToken) {
		n.Nodes = append(n.Nodes, p.shiftComponent())
		p.skipWhitespace()
	}
	if p.at(LeftBraceToken) {
		p.shift()
	}
	p.state = append(p.state, AtRuleState)
	return n
}

func (p *Parser2) parseRuleset() *RulesetNode {
	// check if left brace appears, which is the only check if this is a valid ruleset
	i := 0
	for p.peek(i).TokenType != LeftBraceToken {
		if p.peek(i).TokenType == SemicolonToken || p.peek(i).TokenType == ErrorToken {
			return nil
		}
		i++
	}
	n := NewRuleset()
	for !p.at(LeftBraceToken) && !p.at(ErrorToken) {
		if p.at(CommaToken) {
			p.shift()
			p.skipWhitespace()
			continue
		}
		if cn := p.parseSelector(); cn != nil {
			n.Selectors = append(n.Selectors, cn)
		}
		p.skipWhitespace()
	}
	if p.at(ErrorToken) {
		return nil
	}
	p.shift()
	p.state = append(p.state, RulesetState)
	return n
}

func (p *Parser2) parseSelector() *SelectorNode {
	n := NewSelector()
	var ws *TokenNode
	for !p.at(CommaToken) && !p.at(LeftBraceToken) && !p.at(ErrorToken) {
		if p.at(DelimToken) && (p.data()[0] == '>' || p.data()[0] == '+' || p.data()[0] == '~') {
			n.Elems = append(n.Elems, p.shift())
			p.skipWhitespace()
		} else if p.at(LeftBracketToken) {
			for !p.at(RightBracketToken) && !p.at(ErrorToken) {
				n.Elems = append(n.Elems, p.shift())
				p.skipWhitespace()
			}
			if p.at(RightBracketToken) {
				n.Elems = append(n.Elems, p.shift())
			}
		} else {
			if ws != nil {
				n.Elems = append(n.Elems, ws)
			}
			n.Elems = append(n.Elems, p.shift())
		}

		if p.at(WhitespaceToken) {
			ws = p.shift()
		} else {
			ws = nil
		}
	}
	if len(n.Elems) == 0 {
		return nil
	}
	return n
}

func (p *Parser2) parseDeclaration() *DeclarationNode {
	if !p.at(IdentToken) {
		return nil
	}
	ident := p.shift()
	p.skipWhitespace()
	if !p.at(ColonToken) {
		return nil
	}
	p.shift() // colon
	p.skipWhitespace()
	n := NewDeclaration(ident)
	for !p.at(SemicolonToken) && !p.at(RightBraceToken) && !p.at(ErrorToken) {
		if p.at(DelimToken) && p.data()[0] == '!' {
			exclamation := p.shift()
			p.skipWhitespace()
			if p.at(IdentToken) && bytes.Equal(bytes.ToLower(p.data()), []byte("important")) {
				n.Important = true
				p.shift()
			} else {
				n.Vals = append(n.Vals, exclamation)
			}
		} else if cn := p.parseFunction(); cn != nil {
			n.Vals = append(n.Vals, cn)
		} else {
			n.Vals = append(n.Vals, p.shift())
		}
		p.skipWhitespace()
	}
	p.skipWhile(SemicolonToken)
	return n
}

func (p *Parser2) parseFunction() *FunctionNode {
	if !p.at(FunctionToken) {
		return nil
	}
	n := NewFunction(p.shift())
	p.skipWhitespace()
	for !p.at(RightParenthesisToken) && !p.at(ErrorToken) {
		if p.at(CommaToken) {
			p.shift()
			p.skipWhitespace()
			continue
		}
		n.Args = append(n.Args, p.parseArgument())
	}
	if p.at(ErrorToken) {
		return nil
	}
	p.shift()
	return n
}

func (p *Parser2) parseArgument() *ArgumentNode {
	n := NewArgument()
	for !p.at(CommaToken) && !p.at(RightParenthesisToken) && !p.at(ErrorToken) {
		n.Vals = append(n.Vals, p.shiftComponent())
		p.skipWhitespace()
	}
	return n
}

func (p *Parser2) parseBlock() *BlockNode {
	if !p.at(LeftParenthesisToken) && !p.at(LeftBraceToken) && !p.at(LeftBracketToken) {
		return nil
	}
	n := NewBlock(p.shift())
	p.skipWhitespace()
	for {
		if p.at(RightBraceToken) || p.at(RightParenthesisToken) || p.at(RightBracketToken) || p.at(ErrorToken) {
			break
		}
		n.Nodes = append(n.Nodes, p.shiftComponent())
		p.skipWhitespace()
	}
	if !p.at(ErrorToken) {
		n.Close = p.shift()
	}
	return n
}

func (p *Parser2) shiftComponent() Node {
	if cn := p.parseBlock(); cn != nil {
		return cn
	} else if cn := p.parseFunction(); cn != nil {
		return cn
	} else {
		return p.shift()
	}
}

////////////////////////////////////////////////////////////////

// copyBytes copies bytes to the same position.
// This is required because the referenced slices from the tokenizer might be overwritten on subsequent Next calls.
func (p *Parser2) copy() {
	for _, n := range p.buf[p.pos:] {
		tmp := make([]byte, len(n.Data))
		copy(tmp, n.Data)
		n.Data = tmp
	}
}

func (p *Parser2) read() *TokenNode {
	tt, text := p.z.Next()
	// ignore comments and multiple whitespace
	if tt == CommentToken || tt == WhitespaceToken && len(p.buf) > 0 && p.buf[len(p.buf)-1].TokenType == WhitespaceToken {
		return p.read()
	}
	return NewToken(tt, text)
}

func (p *Parser2) peek(i int) *TokenNode {
	if p.pos+i >= len(p.buf) {
		c := cap(p.buf)
		l := len(p.buf) - p.pos
		if p.pos+i >= c {
			// expand buffer when len is bigger than half the cap
			if 2*l > c {
				// if 2*c > MaxBuf {
				// panic("max buf")
				// 	return NewToken(ErrorToken, []byte("max buffer exceeded"))
				// }
				buf1 := make([]*TokenNode, l, 2*c)
				copy(buf1, p.buf[p.pos:])
				p.buf = buf1
			} else {
				copy(p.buf, p.buf[p.pos:])
				p.buf = p.buf[:l]
			}
			p.pos = 0
			if i >= cap(p.buf) {
				return NewToken(ErrorToken, []byte("looking too far ahead"))
			}
		}
		for j := len(p.buf); j <= p.pos+i; j++ {
			p.buf = append(p.buf, p.read())
		}
	}
	return p.buf[p.pos+i]
}

func (p *Parser2) shift() *TokenNode {
	shifted := p.peek(0)
	p.pos++
	return shifted
}

func (p *Parser2) at(tt TokenType) bool {
	return p.peek(0).TokenType == tt
}

func (p *Parser2) data() []byte {
	return p.peek(0).Data
}

func (p *Parser2) skipWhitespace() {
	if p.at(WhitespaceToken) {
		p.shift()
	}
}

func (p *Parser2) skipWhile(tt TokenType) {
	for p.at(tt) || p.at(WhitespaceToken) {
		p.shift()
	}
}

func (p *Parser2) skipUntil(tt TokenType) {
	for p.at(tt) && !p.at(ErrorToken) {
		p.shift()
	}
}