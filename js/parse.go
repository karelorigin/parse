package js

import (
	"bytes"
	"io"
	"strconv"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/buffer"
)

// TODO for-in extension
// TODO template literal

type Node struct {
	gt    GrammarType
	nodes []Node

	// filled if gt == TokenGrammar
	tt   TokenType
	data []byte
}

// GrammarType determines the type of grammar.
type GrammarType uint32

// GrammarType values.
const (
	ErrorGrammar GrammarType = iota // extra token when errors occur
	ModuleGrammar
	TokenGrammar
	CommentGrammar
	BindingGrammar
	ClauseGrammar
	MethodGrammar
	ExprGrammar
	StmtGrammar
)

// String returns the string representation of a GrammarType.
func (tt GrammarType) String() string {
	switch tt {
	case ErrorGrammar:
		return "Error"
	case ModuleGrammar:
		return "Module"
	case TokenGrammar:
		return "Token"
	case CommentGrammar:
		return "Comment"
	case BindingGrammar:
		return "Binding"
	case ClauseGrammar:
		return "Clause"
	case MethodGrammar:
		return "Method"
	case ExprGrammar:
		return "Expr"
	case StmtGrammar:
		return "Stmt"
	}
	return "Invalid(" + strconv.Itoa(int(tt)) + ")"
}

////////////////////////////////////////////////////////////////

// Parser is the state for the parser.
type Parser struct {
	l   *Lexer
	err error

	tt                 TokenType
	data               []byte
	prevLineTerminator bool
}

// Parse returns a JS AST tree of.
func Parse(r io.Reader) (Node, error) {
	l := NewLexer(r)
	defer l.Restore()

	p := &Parser{l: l}

	p.tt = WhitespaceToken // trick so that next() works from the start
	p.next()

	module := p.parseModule()
	if p.err == nil {
		p.err = p.l.Err()
	}
	return module, p.err
}

////////////////////////////////////////////////////////////////

func (p *Parser) next() {
	if p.tt == ErrorToken {
		return
	}
	p.prevLineTerminator = false

	p.tt, p.data = p.l.Next()
	for p.tt == WhitespaceToken || p.tt == LineTerminatorToken {
		if p.tt == LineTerminatorToken {
			p.prevLineTerminator = true
		}
		p.tt, p.data = p.l.Next()
	}
}

func (p *Parser) fail(in string, expected ...TokenType) {
	if p.err == nil {
		s := "unexpected"
		if 0 < len(expected) {
			s = "expected"
			for i, tt := range expected[:len(expected)-1] {
				if 0 < i {
					s += ","
				}
				s += " '" + tt.String() + "'"
			}
			if 2 < len(expected) {
				s += ", or"
			} else if 1 < len(expected) {
				s += " or"
			}
			s += " '" + expected[len(expected)-1].String() + "' instead of"
		}

		at := "'" + string(p.data) + "'"
		if p.tt == ErrorToken {
			at = p.l.Err().Error()
		}

		offset := p.l.r.Offset() - len(p.data)
		p.err = parse.NewError(buffer.NewReader(p.l.r.Bytes()), offset, "%s %s in %s", s, at, in)
		p.tt = ErrorToken
		p.data = nil
	}
}

func (p *Parser) consume(in string, tt TokenType) bool {
	if p.tt != tt {
		p.fail(in, tt)
		return false
	}
	p.next()
	return true
}

func (p *Parser) parseModule() Node {
	nodes := []Node{}
	for {
		switch p.tt {
		case ErrorToken:
			return Node{ModuleGrammar, nodes, 0, nil}
		case ImportToken, ExportToken:
			panic("import and export statements not implemented") // TODO
		default:
			nodes = append(nodes, p.parseStmt())
		}
	}
}

func (p *Parser) parseStmt() Node {
	nodes := []Node{}
	switch p.tt {
	case OpenBraceToken:
		return p.parseBlockStmt("block statement")
	case LetToken, ConstToken, VarToken:
		nodes = p.parseVarDecl(nodes)
	case ContinueToken, BreakToken:
		nodes = append(nodes, p.parseToken())
		if !p.prevLineTerminator && (p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken) {
			nodes = append(nodes, p.parseToken())
		}
	case ReturnToken:
		nodes = append(nodes, p.parseToken())
		if !p.prevLineTerminator && p.tt != SemicolonToken && p.tt != LineTerminatorToken && p.tt != ErrorToken {
			nodes = append(nodes, p.parseExpr())
		}
	case IfToken:
		nodes = append(nodes, p.parseToken())
		if !p.consume("if statement", OpenParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseExpr())
		if !p.consume("if statement", CloseParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseStmt())
		if p.tt == ElseToken {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseStmt())
		}
	case WithToken:
		nodes = append(nodes, p.parseToken())
		if !p.consume("with statement", OpenParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseExpr())
		if !p.consume("with statement", CloseParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseStmt())
	case DoToken:
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseStmt())
		if p.tt != WhileToken {
			p.fail("do statement", WhileToken)
			return Node{}
		}
		nodes = append(nodes, p.parseToken())
		if !p.consume("do statement", OpenParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseExpr())
		if !p.consume("do statement", CloseParenToken) {
			return Node{}
		}
	case WhileToken:
		nodes = append(nodes, p.parseToken())
		if !p.consume("while statement", OpenParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseExpr())
		if !p.consume("while statement", CloseParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseStmt())
	case ForToken:
		nodes = append(nodes, p.parseToken())
		if p.tt == AwaitToken {
			nodes = append(nodes, p.parseToken())
		}
		if !p.consume("for statement", OpenParenToken) {
			return Node{}
		}
		if p.tt == VarToken || p.tt == LetToken || p.tt == ConstToken {
			declNodes := []Node{}
			declNodes = p.parseVarDecl(declNodes)
			nodes = append(nodes, Node{StmtGrammar, declNodes, 0, nil})
		} else {
			nodes = append(nodes, Node{ExprGrammar, p.parseLeftHandSideExpr(nil), 0, nil})
		}

		if p.tt == SemicolonToken {
			p.next()
			nodes = append(nodes, p.parseExpr())
			if !p.consume("for statement", SemicolonToken) {
				return Node{}
			}
			nodes = append(nodes, p.parseExpr())
		} else if p.tt == InToken {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseExpr())
		} else if p.tt == IdentifierToken && bytes.Equal(p.data, []byte("of")) {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseAssignmentExpr())
		} else {
			p.fail("for statement", InToken, OfToken, SemicolonToken)
			return Node{}
		}
		if !p.consume("for statement", CloseParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseStmt())
	case IdentifierToken, YieldToken, AwaitToken:
		// could be expression or labelled statement, try expression first and convert to labelled statement if possible
		expr := p.parseExpr()
		if p.tt == ColonToken && len(expr.nodes) == 1 {
			nodes = append(nodes, expr.nodes[0])
			p.next()
			nodes = append(nodes, p.parseStmt())
		} else {
			nodes = append(nodes, expr)
		}
	case SwitchToken:
		nodes = append(nodes, p.parseToken())
		if !p.consume("switch statement", OpenParenToken) {
			return Node{}
		}
		nodes = append(nodes, p.parseExpr())
		if !p.consume("switch statement", CloseParenToken) {
			return Node{}
		}

		// case block
		if !p.consume("switch statement", OpenBraceToken) {
			return Node{}
		}
		for p.tt != ErrorToken {
			if p.tt == CloseBraceToken {
				p.next()
				break
			}

			clauseNodes := []Node{}
			if p.tt == CaseToken {
				clauseNodes = append(clauseNodes, p.parseToken())
				clauseNodes = append(clauseNodes, p.parseExpr())
			} else if p.tt == DefaultToken {
				clauseNodes = append(clauseNodes, p.parseToken())
			} else {
				p.fail("switch statement", CaseToken, DefaultToken)
				return Node{}
			}
			if !p.consume("switch statement", ColonToken) {
				return Node{}
			}
			for p.tt != CaseToken && p.tt != DefaultToken && p.tt != CloseBraceToken && p.tt != ErrorToken {
				clauseNodes = append(clauseNodes, p.parseStmt())
			}
			nodes = append(nodes, Node{ClauseGrammar, clauseNodes, 0, nil})
		}
	case FunctionToken:
		nodes = p.parseFuncDecl(nodes)
	case AsyncToken: // async function
		nodes = append(nodes, p.parseToken())
		if p.tt != FunctionToken {
			p.fail("async function statement", FunctionToken)
			return Node{}
		}
		nodes = p.parseFuncDecl(nodes)
	case ClassToken:
		nodes = p.parseClassDecl(nodes)
	case ThrowToken:
		nodes = append(nodes, p.parseToken())
		if !p.prevLineTerminator {
			nodes = append(nodes, p.parseExpr())
		}
	case TryToken:
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseBlockStmt("try statement"))

		if p.tt == CatchToken {
			nodes = append(nodes, p.parseToken())
			if p.tt == OpenParenToken {
				p.next()
				nodes = append(nodes, p.parseBinding())
				if p.tt != CloseParenToken {
					p.fail("try statement", CloseParenToken)
					return Node{}
				}
				p.next()
			}
			nodes = append(nodes, p.parseBlockStmt("try statement"))
		}
		if p.tt == FinallyToken {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseBlockStmt("try statement"))
		}
	case DebuggerToken:
		nodes = append(nodes, p.parseToken())
	case SemicolonToken, LineTerminatorToken:
		// empty
	default:
		expr := p.parseExpr()
		if 0 < len(expr.nodes) {
			nodes = append(nodes, expr)
		} else {
			p.fail("statement")
			return Node{}
		}
	}
	if p.tt == SemicolonToken || p.tt == LineTerminatorToken {
		p.next()
	}
	return Node{StmtGrammar, nodes, 0, nil}
}

func (p *Parser) parseVarDecl(nodes []Node) []Node {
	// assume we're at var, let or const
	nodes = append(nodes, p.parseToken())
	for {
		nodes = append(nodes, p.parseBindingElement())
		if p.tt == CommaToken {
			p.next()
		} else {
			break
		}
	}
	return nodes
}

func (p *Parser) parseFuncDecl(nodes []Node) []Node {
	// assume we're at function
	nodes = append(nodes, p.parseToken())
	if p.tt == MulToken {
		nodes = append(nodes, p.parseToken())
	}
	if p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken {
		nodes = append(nodes, p.parseToken())
	}
	nodes = p.parseFuncParams("function declaration", nodes)
	nodes = append(nodes, p.parseBlockStmt("function declaration"))
	return nodes
}

func (p *Parser) parseFuncParams(in string, nodes []Node) []Node {
	if !p.consume(in, OpenParenToken) {
		return nil
	}

	for p.tt != CloseParenToken {
		// binding rest element
		if p.tt == EllipsisToken {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseBinding())
			break
		}

		nodes = append(nodes, p.parseBindingElement())

		if p.tt == CommaToken {
			p.next()
		} else if p.tt == CloseParenToken {
			break
		} else {
			p.fail(in, CommaToken, CloseParenToken)
			return nil
		}
	}
	if !p.consume(in, CloseParenToken) {
		return nil
	}
	return nodes
}

func (p *Parser) parseBlockStmt(in string) Node {
	if p.tt != OpenBraceToken {
		p.fail(in, OpenBraceToken)
		return Node{}
	}
	nodes := []Node{}
	nodes = append(nodes, p.parseToken())
	for p.tt != ErrorToken {
		if p.tt == CloseBraceToken {
			nodes = append(nodes, p.parseToken())
			break
		}
		nodes = append(nodes, p.parseStmt())
	}
	return Node{StmtGrammar, nodes, 0, nil}
}

func (p *Parser) parseClassDecl(nodes []Node) []Node {
	// assume we're at class
	nodes = append(nodes, p.parseToken())
	if p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken {
		nodes = append(nodes, p.parseToken())
	}
	if p.tt == ExtendsToken {
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, Node{ExprGrammar, p.parseLeftHandSideExpr(nil), 0, nil})
	}

	if !p.consume("class statement", OpenBraceToken) {
		return nil
	}
	for p.tt != ErrorToken {
		if p.tt == SemicolonToken {
			p.next()
			continue
		} else if p.tt == CloseBraceToken {
			break
		}
		nodes = append(nodes, p.parseMethodDef())
	}
	if !p.consume("class statement", CloseBraceToken) {
		return nil
	}
	return nodes
}

func (p *Parser) parseMethodDef() Node {
	nodes := []Node{}
	if p.tt == StaticToken {
		nodes = append(nodes, p.parseToken())
	}
	if p.tt == AsyncToken || p.tt == MulToken {
		if p.tt == AsyncToken {
			nodes = append(nodes, p.parseToken())
		}
		if p.tt == MulToken {
			nodes = append(nodes, p.parseToken())
		}
	} else if p.tt == IdentifierToken && (bytes.Equal(p.data, []byte("get")) || bytes.Equal(p.data, []byte("set"))) {
		nodes = append(nodes, p.parseToken())
	}

	if IsIdentifier(p.tt) || p.tt == StringToken || p.tt == NumericToken {
		nodes = append(nodes, p.parseToken())
	} else if p.tt == OpenBracketToken {
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseAssignmentExpr())
		if p.tt != CloseBracketToken {
			p.fail("method definition", CloseBracketToken)
			return Node{}
		}
		nodes = append(nodes, p.parseToken())
	} else {
		p.fail("method definition", IdentifierToken, StringToken, NumericToken, OpenBracketToken)
		return Node{}
	}
	nodes = p.parseFuncParams("method definition", nodes)
	nodes = append(nodes, p.parseBlockStmt("method definition"))
	return Node{MethodGrammar, nodes, 0, nil}
}

func (p *Parser) parseBindingElement() Node {
	// binding element
	binding := p.parseBinding()
	if p.tt == EqToken {
		binding.nodes = append(binding.nodes, p.parseToken())
		binding.nodes = append(binding.nodes, p.parseAssignmentExpr())
	}
	return binding
}

func (p *Parser) parseBinding() Node {
	// binding identifier or binding pattern
	nodes := []Node{}
	if p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken {
		nodes = append(nodes, p.parseToken())
	} else if p.tt == OpenBracketToken {
		nodes = append(nodes, p.parseToken())
		for p.tt != CloseBracketToken {
			// elision
			for p.tt == CommaToken {
				p.next()
			}
			// binding rest element
			if p.tt == EllipsisToken {
				nodes = append(nodes, p.parseToken())
				nodes = append(nodes, p.parseBinding())
				if p.tt != CloseBracketToken {
					p.fail("array binding pattern", CloseBracketToken)
					return Node{}
				}
				break
			}

			nodes = append(nodes, p.parseBindingElement())

			if p.tt == CommaToken {
				for p.tt == CommaToken {
					p.next()
				}
			} else if p.tt != CloseBracketToken {
				p.fail("array binding pattern", CommaToken, CloseBracketToken)
				return Node{}
			}
		}
		nodes = append(nodes, p.parseToken())
	} else if p.tt == OpenBraceToken {
		nodes = append(nodes, p.parseToken())
		for p.tt != CloseBraceToken {
			// binding rest property
			if p.tt == EllipsisToken {
				nodes = append(nodes, p.parseToken())
				if p.tt != IdentifierToken && p.tt != YieldToken && p.tt != AwaitToken {
					p.fail("object binding pattern", IdentifierToken)
				}
				nodes = append(nodes, Node{BindingGrammar, []Node{p.parseToken()}, 0, nil})
				if p.tt != CloseBraceToken {
					p.fail("object binding pattern", CloseBraceToken)
					return Node{}
				}
				break
			}

			if p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken {
				// single name binding
				ident := p.parseToken()
				if p.tt == ColonToken {
					// property name + : + binding element
					nodes = append(nodes, ident)
					nodes = append(nodes, p.parseToken())
					nodes = append(nodes, p.parseBindingElement())
				} else {
					binding := []Node{ident}
					if p.tt == EqToken {
						binding = append(binding, p.parseToken())
						binding = append(binding, p.parseAssignmentExpr())
					}
					nodes = append(nodes, Node{BindingGrammar, binding, 0, nil})
				}
			} else if IsIdentifier(p.tt) || p.tt == StringToken || p.tt == NumericToken || p.tt == OpenBracketToken {
				// property name + : + binding element
				if p.tt == OpenBracketToken {
					nodes = append(nodes, p.parseToken())
					nodes = append(nodes, p.parseAssignmentExpr())
					if p.tt != CloseBracketToken {
						p.fail("object binding pattern", CloseBracketToken)
						return Node{}
					}
					nodes = append(nodes, p.parseToken())
				} else {
					nodes = append(nodes, p.parseToken())
				}
				if p.tt != ColonToken {
					p.fail("object binding pattern", ColonToken)
					return Node{}
				}
				nodes = append(nodes, p.parseToken())
				nodes = append(nodes, p.parseBindingElement())
			} else {
				p.fail("object binding pattern", IdentifierToken, StringToken, NumericToken, OpenBracketToken)
				return Node{}
			}

			if p.tt == CommaToken {
				p.next()
			} else if p.tt != CloseBraceToken {
				p.fail("object binding pattern", CommaToken, CloseBraceToken)
				return Node{}
			}
		}
		nodes = append(nodes, p.parseToken())
	} else {
		p.fail("binding")
		return Node{}
	}
	return Node{BindingGrammar, nodes, 0, nil}
}

func (p *Parser) parseObjectLiteral(nodes []Node) []Node {
	// assume we're on {
	nodes = append(nodes, p.parseToken())
	for p.tt != CloseBraceToken && p.tt != ErrorToken {
		if p.tt == EllipsisToken {
			nodes = append(nodes, p.parseToken())
			nodes = append(nodes, p.parseAssignmentExpr())
		} else if p.tt == CommaToken {
			nodes = append(nodes, p.parseToken())
		} else {
			property := []Node{}
			for p.tt == MulToken || p.tt == AsyncToken || IsIdentifier(p.tt) {
				property = append(property, p.parseToken())
			}

			if (p.tt == EqToken || p.tt == CommaToken || p.tt == CloseBraceToken) && len(property) == 1 && (property[0].tt == IdentifierToken || property[0].tt == YieldToken || property[0].tt == AwaitToken) {
				nodes = append(nodes, property[0])
				if p.tt == EqToken {
					nodes = append(nodes, p.parseToken())
					nodes = append(nodes, p.parseAssignmentExpr())
				}
			} else if 0 < len(property) && IsIdentifier(property[len(property)-1].tt) || p.tt == StringToken || p.tt == NumericToken || p.tt == OpenBracketToken {
				if p.tt == StringToken || p.tt == NumericToken {
					property = append(property, p.parseToken())
				} else if p.tt == OpenBracketToken {
					property = append(property, p.parseToken())
					property = append(property, p.parseAssignmentExpr())
					if p.tt != CloseBracketToken {
						p.fail("object literal", CloseBracketToken)
						return nil
					}
					property = append(property, p.parseToken())
				}

				if p.tt == ColonToken {
					nodes = append(nodes, property...)
					nodes = append(nodes, p.parseToken())
					nodes = append(nodes, p.parseAssignmentExpr())
				} else if p.tt == OpenParenToken {
					property = p.parseFuncParams("method definition", property)
					property = append(property, p.parseBlockStmt("method definition"))
					nodes = append(nodes, Node{MethodGrammar, property, 0, nil})
				} else {
					p.fail("object literal", ColonToken, OpenParenToken)
					return nil
				}
			} else {
				p.fail("object literal", EqToken, CommaToken, CloseBraceToken, EllipsisToken, IdentifierToken, StringToken, NumericToken, OpenBracketToken)
				return nil
			}
		}
	}
	if p.tt == CloseBraceToken {
		nodes = append(nodes, p.parseToken())
	}
	return nodes
}

func (p *Parser) parseTemplateLiteral(nodes []Node) []Node {
	// assume we're on 'Template' or 'TemplateStart'
	for p.tt == TemplateStartToken || p.tt == TemplateMiddleToken {
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseExpr())
		if p.tt == TemplateEndToken {
			nodes = append(nodes, p.parseToken())
			return nodes
		} else {
			p.fail("template literal", TemplateToken)
			return nil
		}
	}
	nodes = append(nodes, p.parseToken())
	return nodes
}

func (p *Parser) parsePrimaryExpr(nodes []Node) []Node {
	// reparse input if we have / or /= as the beginning of a new expression, this should be a regular expression!
	if p.tt == DivToken || p.tt == DivEqToken {
		p.tt, p.data = p.l.RegExp()
	}

	switch p.tt {
	case ThisToken, IdentifierToken, YieldToken, AwaitToken, NullToken, TrueToken, FalseToken, NumericToken, StringToken, RegExpToken:
		nodes = append(nodes, p.parseToken())
	case TemplateToken, TemplateStartToken:
		nodes = p.parseTemplateLiteral(nodes)
	case OpenBracketToken:
		// array literal and [expression]
		nodes = append(nodes, p.parseToken())
		for p.tt != CloseBracketToken && p.tt != ErrorToken {
			if p.tt == EllipsisToken || p.tt == CommaToken {
				nodes = append(nodes, p.parseToken())
			} else {
				nodes = append(nodes, p.parseAssignmentExpr())
			}
		}
		nodes = append(nodes, p.parseToken())
	case OpenBraceToken:
		nodes = p.parseObjectLiteral(nodes)
	case OpenParenToken:
		// parenthesized expression and arrow parameter list
		nodes = append(nodes, p.parseToken())
		for p.tt != CloseParenToken && p.tt != ErrorToken {
			if p.tt == EllipsisToken {
				nodes = append(nodes, p.parseToken())
				nodes = append(nodes, p.parseBinding())
			} else if p.tt == CommaToken {
				nodes = append(nodes, p.parseToken())
			} else {
				nodes = append(nodes, p.parseAssignmentExpr())
			}
		}
		nodes = append(nodes, p.parseToken())
	case ClassToken:
		nodes = p.parseClassDecl(nodes)
	case FunctionToken:
		nodes = p.parseFuncDecl(nodes)
	case AsyncToken:
		// async function
		nodes = append(nodes, p.parseToken())
		if !p.prevLineTerminator {
			if p.tt == FunctionToken {
				nodes = p.parseFuncDecl(nodes)
			} else {
				p.fail("async function expression", FunctionToken)
				return nil
			}
		}
	default:
		p.fail("expression")
		return nil
	}
	return nodes
}

func (p *Parser) parseLeftHandSideExprEnd(nodes []Node) []Node {
	// parse arguments, [expression], .identifier, template
	if p.tt == OpenParenToken {
		nodes = append(nodes, p.parseToken())
		for {
			if p.tt == ErrorToken || p.tt == CloseParenToken {
				break
			} else if p.tt == CommaToken {
				p.next()
			} else if p.tt == EllipsisToken {
				nodes = append(nodes, p.parseToken())
				nodes = append(nodes, p.parseAssignmentExpr())
				break
			} else {
				nodes = append(nodes, p.parseAssignmentExpr())
			}
		}
		if p.tt == CommaToken {
			p.next()
		}
		if p.tt != CloseParenToken {
			p.fail("left hand side expression", CloseParenToken)
			return nil
		}
		nodes = append(nodes, p.parseToken())
	} else if p.tt == OpenBracketToken {
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseExpr())
		if p.tt != CloseBracketToken {
			p.fail("left hand side expression", CloseBracketToken)
			return nil
		}
		nodes = append(nodes, p.parseToken())
	} else if p.tt == DotToken {
		nodes = append(nodes, p.parseToken())
		if !IsIdentifier(p.tt) {
			p.fail("left hand side expression", IdentifierToken)
			return nil
		}
		nodes = append(nodes, p.parseToken())
	} else if p.tt == TemplateToken || p.tt == TemplateStartToken {
		nodes = p.parseTemplateLiteral(nodes)
	} else {
		p.fail("left hand side expression", OpenParenToken, OpenBracketToken, DotToken, TemplateToken)
		return nil
	}
	return nodes
}

func (p *Parser) parseLeftHandSideExpr(nodes []Node) []Node {
	for p.tt == NewToken {
		nodes = append(nodes, p.parseToken())
		if p.tt == DotToken {
			nodes = append(nodes, p.parseToken())
			if p.tt != IdentifierToken || !bytes.Equal(p.data, []byte("target")) {
				p.fail("left hand side expression", TargetToken)
				return nil
			}
			nodes = append(nodes, p.parseToken())
			goto LHSEND
		}
	}

	if p.tt == SuperToken {
		nodes = append(nodes, p.parseToken())
		if p.tt == TemplateToken || p.tt == TemplateStartToken {
			p.fail("left hand side expression")
		}
		nodes = p.parseLeftHandSideExprEnd(nodes)
	} else if p.tt == ImportToken {
		nodes = append(nodes, p.parseToken())
		if p.tt != OpenParenToken {
			p.fail("left hand side expression", OpenParenToken)
			return nil
		}
		p.next()
		nodes = append(nodes, p.parseExpr())
		if p.tt != CloseParenToken {
			p.fail("left hand side expression", CloseParenToken)
			return nil
		}
		p.next()
	} else {
		nodes = p.parsePrimaryExpr(nodes)
	}

LHSEND:
	// parse arguments, [expression], .identifier, template at the end of member expressions and call expressions
	for p.tt == OpenParenToken || p.tt == OpenBracketToken || p.tt == DotToken || p.tt == TemplateToken || p.tt == TemplateStartToken {
		nodes = p.parseLeftHandSideExprEnd(nodes)
	}

	// parse optional chaining at the end of left hand expressions
	for p.tt == OptChainToken {
		nodes = append(nodes, p.parseToken())
		if IsIdentifier(p.tt) {
			nodes = append(nodes, p.parseToken())
		} else if p.tt == OpenParenToken || p.tt == OpenBracketToken || p.tt == TemplateToken || p.tt == TemplateStartToken {
			nodes = p.parseLeftHandSideExprEnd(nodes)
		} else {
			p.fail("left hand side expression", IdentifierToken, OpenParenToken, OpenBracketToken, TemplateToken)
			return nil
		}
		for p.tt == OpenParenToken || p.tt == OpenBracketToken || p.tt == DotToken || p.tt == TemplateToken || p.tt == TemplateStartToken {
			nodes = p.parseLeftHandSideExprEnd(nodes)
		}
	}
	return nodes
}

func (p *Parser) parseAssignmentExpr() Node {
	nodes := []Node{}
	if p.tt == YieldToken {
		nodes = append(nodes, p.parseToken())
		if p.tt == ArrowToken {
			nodes[len(nodes)-1] = Node{BindingGrammar, []Node{nodes[len(nodes)-1]}, 0, nil}
			nodes = append(nodes, p.parseToken())
			if p.tt == OpenBraceToken {
				nodes = append(nodes, p.parseBlockStmt("arrow function expression"))
			} else {
				nodes = append(nodes, p.parseAssignmentExpr())
			}
		} else if !p.prevLineTerminator {
			if p.tt == MulToken {
				nodes = append(nodes, p.parseToken())
			}
			nodes = append(nodes, p.parseAssignmentExpr())
		}
		return Node{ExprGrammar, nodes, 0, nil}
	} else if p.tt == AsyncToken {
		nodes = append(nodes, p.parseToken())
		if p.prevLineTerminator {
			p.fail("async function expression")
			return Node{}
		}
		if p.tt == FunctionToken {
			// primary expression
			nodes = p.parseFuncDecl(nodes)
		} else if p.tt == IdentifierToken || p.tt == YieldToken || p.tt == AwaitToken {
			nodes = append(nodes, Node{BindingGrammar, []Node{p.parseToken()}, 0, nil})
			if p.tt != ArrowToken {
				p.fail("async arrow function expression", ArrowToken)
				return Node{}
			}
			nodes = append(nodes, p.parseToken())
			if p.tt == OpenBraceToken {
				nodes = append(nodes, p.parseBlockStmt("async arrow function expression"))
			} else {
				nodes = append(nodes, p.parseAssignmentExpr())
			}
		} else {
			p.fail("async function expression", FunctionToken, IdentifierToken)
			return Node{}
		}
		return Node{ExprGrammar, nodes, 0, nil}
	}

LHS:
	if p.tt == IncrToken || p.tt == DecrToken {
		nodes = append(nodes, p.parseToken())
	}
	nodes = p.parseLeftHandSideExpr(nodes)
	if !p.prevLineTerminator && (p.tt == IncrToken || p.tt == DecrToken) {
		nodes = append(nodes, p.parseToken())
	}
	switch p.tt {
	case NullishToken, OrToken, AndToken, BitOrToken, BitXorToken, BitAndToken, EqEqToken, NotEqToken, EqEqEqToken, NotEqEqToken, LtToken, GtToken, LtEqToken, GtEqToken, LtLtToken, GtGtToken, GtGtGtToken, AddToken, SubToken, MulToken, DivToken, ModToken, ExpToken, NotToken, BitNotToken, InstanceofToken, InToken, TypeofToken, VoidToken, DeleteToken:
		nodes = append(nodes, p.parseToken())
		goto LHS
	case EqToken, MulEqToken, DivEqToken, ModEqToken, ExpEqToken, AddEqToken, SubEqToken, LtLtEqToken, GtGtEqToken, GtGtGtEqToken, BitAndEqToken, BitXorEqToken, BitOrEqToken:
		// we allow the left-hand-side to be a full assignment expression instead of a left-hand-side expression, but that's fine
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseAssignmentExpr())
	case QuestionToken:
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseAssignmentExpr())
		if p.tt != ColonToken {
			p.fail("conditional expression", ColonToken)
			return Node{}
		}
		nodes = append(nodes, p.parseToken())
		nodes = append(nodes, p.parseAssignmentExpr())
	case ArrowToken:
		// we allow the start of an arrow function expressions to be anything in a left-hand-side expression, but that should be fine
		// previous token should be identifier, yield, await, or arrow parameter list (end with CloseParenToken)
		if nodes[len(nodes)-1].tt == IdentifierToken || nodes[len(nodes)-1].tt == YieldToken || nodes[len(nodes)-1].tt == AwaitToken {
			nodes[len(nodes)-1] = Node{BindingGrammar, []Node{nodes[len(nodes)-1]}, 0, nil}
		} else if nodes[len(nodes)-1].tt != CloseParenToken {
			p.fail("arrow function expression")
			return Node{}
		}
		nodes = append(nodes, p.parseToken())
		if p.tt == OpenBraceToken {
			nodes = append(nodes, p.parseBlockStmt("arrow function expression"))
		} else {
			nodes = append(nodes, p.parseAssignmentExpr())
		}
	}
	return Node{ExprGrammar, nodes, 0, nil}
}

func (p *Parser) parseExpr() Node {
	node := p.parseAssignmentExpr()
	for p.tt == CommaToken {
		p.next()
		node.nodes = append(node.nodes, p.parseAssignmentExpr().nodes...)
	}
	return node
}

func (p *Parser) parseToken() Node {
	node := Node{TokenGrammar, nil, p.tt, p.data}
	p.next()
	return node
}