package sieve

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Parser parses Sieve scripts into executable rules
type Parser struct {
	input  string
	pos    int
	tokens []token
}

type token struct {
	typ tokenType
	val string
}

type tokenType int

const (
	tokenEOF tokenType = iota
	tokenRequire
	tokenIf
	tokenElsif
	tokenElse
	tokenAllof
	tokenAnyof
	tokenNot
	tokenTrue
	tokenFalse
	tokenAddress
	tokenHeader
	tokenSize
	tokenExists
	tokenContains
	tokenIs
	tokenMatches
	tokenOver
	tokenUnder
	tokenKeep
	tokenFileinto
	tokenRedirect
	tokenDiscard
	tokenReject
	tokenVacation
	tokenStop
	tokenString
	tokenNumber
	tokenLBracket // [
	tokenRBracket // ]
	tokenLBrace   // {
	tokenRBrace   // }
	tokenLParen   // (
	tokenRParen   // )
	tokenSemi     // ;
	tokenComma    // ,
	tokenColon    // :
)

// Parse parses a Sieve script string into a ParsedScript
func Parse(script string) (*ParsedScript, error) {
	p := &Parser{input: script}
	if err := p.tokenize(); err != nil {
		return nil, err
	}
	return p.parse()
}

func (p *Parser) tokenize() error {
	// Simple tokenizer for Sieve subset
	s := p.input
	s = regexp.MustCompile(`#[^\n]*`).ReplaceAllString(s, "") // Remove comments
	s = regexp.MustCompile(`/\*[\s\S]*?\*/`).ReplaceAllString(s, "") // Remove block comments

	// Keywords regex
	keywords := map[string]tokenType{
		"require":  tokenRequire,
		"if":       tokenIf,
		"elsif":    tokenElsif,
		"else":     tokenElse,
		"allof":    tokenAllof,
		"anyof":    tokenAnyof,
		"not":      tokenNot,
		"true":     tokenTrue,
		"false":    tokenFalse,
		"address":  tokenAddress,
		"header":   tokenHeader,
		"size":     tokenSize,
		"exists":   tokenExists,
		"contains": tokenContains,
		"is":       tokenIs,
		"matches":  tokenMatches,
		"over":     tokenOver,
		"under":    tokenUnder,
		"keep":     tokenKeep,
		"fileinto": tokenFileinto,
		"redirect": tokenRedirect,
		"discard":  tokenDiscard,
		"reject":   tokenReject,
		"vacation": tokenVacation,
		"stop":     tokenStop,
	}

	i := 0
	for i < len(s) {
		// Skip whitespace
		for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
			i++
		}
		if i >= len(s) {
			break
		}

		ch := s[i]

		// Single character tokens
		switch ch {
		case '[':
			p.tokens = append(p.tokens, token{tokenLBracket, "["})
			i++
			continue
		case ']':
			p.tokens = append(p.tokens, token{tokenRBracket, "]"})
			i++
			continue
		case '{':
			p.tokens = append(p.tokens, token{tokenLBrace, "{"})
			i++
			continue
		case '}':
			p.tokens = append(p.tokens, token{tokenRBrace, "}"})
			i++
			continue
		case '(':
			p.tokens = append(p.tokens, token{tokenLParen, "("})
			i++
			continue
		case ')':
			p.tokens = append(p.tokens, token{tokenRParen, ")"})
			i++
			continue
		case ';':
			p.tokens = append(p.tokens, token{tokenSemi, ";"})
			i++
			continue
		case ',':
			p.tokens = append(p.tokens, token{tokenComma, ","})
			i++
			continue
		case ':':
			p.tokens = append(p.tokens, token{tokenColon, ":"})
			i++
			continue
		}

		// String literal
		if ch == '"' {
			i++
			start := i
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) {
					i++ // Skip escaped char
				}
				i++
			}
			val := s[start:i]
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
			p.tokens = append(p.tokens, token{tokenString, val})
			i++ // Skip closing quote
			continue
		}

		// Number
		if ch >= '0' && ch <= '9' {
			start := i
			for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == 'K' || s[i] == 'M' || s[i] == 'G') {
				i++
			}
			p.tokens = append(p.tokens, token{tokenNumber, s[start:i]})
			continue
		}

		// Keyword or identifier
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			start := i
			for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= '0' && s[i] <= '9') || s[i] == '_') {
				i++
			}
			word := strings.ToLower(s[start:i])
			if typ, ok := keywords[word]; ok {
				p.tokens = append(p.tokens, token{typ, word})
			} else {
				p.tokens = append(p.tokens, token{tokenString, word})
			}
			continue
		}

		i++ // Skip unknown character
	}

	p.tokens = append(p.tokens, token{tokenEOF, ""})
	return nil
}

func (p *Parser) parse() (*ParsedScript, error) {
	script := &ParsedScript{}

	for p.pos < len(p.tokens) && p.tokens[p.pos].typ != tokenEOF {
		tok := p.tokens[p.pos]

		switch tok.typ {
		case tokenRequire:
			reqs, err := p.parseRequire()
			if err != nil {
				return nil, err
			}
			script.Require = append(script.Require, reqs...)

		case tokenIf:
			rule, err := p.parseRule()
			if err != nil {
				return nil, err
			}
			script.Rules = append(script.Rules, *rule)

		default:
			p.pos++
		}
	}

	return script, nil
}

func (p *Parser) parseRequire() ([]string, error) {
	p.pos++ // skip 'require'

	var reqs []string

	if p.tokens[p.pos].typ == tokenLBracket {
		// Array of requirements
		p.pos++
		for p.tokens[p.pos].typ != tokenRBracket && p.tokens[p.pos].typ != tokenEOF {
			if p.tokens[p.pos].typ == tokenString {
				reqs = append(reqs, p.tokens[p.pos].val)
			}
			p.pos++
		}
		p.pos++ // skip ']'
	} else if p.tokens[p.pos].typ == tokenString {
		reqs = append(reqs, p.tokens[p.pos].val)
		p.pos++
	}

	if p.tokens[p.pos].typ == tokenSemi {
		p.pos++
	}

	return reqs, nil
}

func (p *Parser) parseRule() (*Rule, error) {
	rule := &Rule{}
	p.pos++ // skip 'if'

	// Parse condition
	cond, allOf, err := p.parseCondition()
	if err != nil {
		return nil, err
	}
	rule.Conditions = cond
	rule.AllOf = allOf

	// Parse actions block
	if p.tokens[p.pos].typ != tokenLBrace {
		return nil, fmt.Errorf("expected '{' after condition")
	}
	p.pos++

	for p.tokens[p.pos].typ != tokenRBrace && p.tokens[p.pos].typ != tokenEOF {
		action, err := p.parseAction()
		if err != nil {
			return nil, err
		}
		if action != nil {
			rule.Actions = append(rule.Actions, action)
		}
	}
	p.pos++ // skip '}'

	return rule, nil
}

func (p *Parser) parseCondition() ([]Condition, bool, error) {
	var conditions []Condition
	allOf := true

	tok := p.tokens[p.pos]

	switch tok.typ {
	case tokenAllof:
		p.pos++
		allOf = true
		if p.tokens[p.pos].typ == tokenLParen {
			p.pos++
			for p.tokens[p.pos].typ != tokenRParen && p.tokens[p.pos].typ != tokenEOF {
				cond, err := p.parseSingleCondition()
				if err != nil {
					return nil, false, err
				}
				if cond != nil {
					conditions = append(conditions, cond)
				}
				if p.tokens[p.pos].typ == tokenComma {
					p.pos++
				}
			}
			p.pos++ // skip ')'
		}

	case tokenAnyof:
		p.pos++
		allOf = false
		if p.tokens[p.pos].typ == tokenLParen {
			p.pos++
			for p.tokens[p.pos].typ != tokenRParen && p.tokens[p.pos].typ != tokenEOF {
				cond, err := p.parseSingleCondition()
				if err != nil {
					return nil, false, err
				}
				if cond != nil {
					conditions = append(conditions, cond)
				}
				if p.tokens[p.pos].typ == tokenComma {
					p.pos++
				}
			}
			p.pos++ // skip ')'
		}

	case tokenTrue:
		p.pos++
		conditions = append(conditions, &TrueCondition{})

	case tokenFalse:
		p.pos++
		conditions = append(conditions, &FalseCondition{})

	default:
		cond, err := p.parseSingleCondition()
		if err != nil {
			return nil, false, err
		}
		if cond != nil {
			conditions = append(conditions, cond)
		}
	}

	return conditions, allOf, nil
}

func (p *Parser) parseSingleCondition() (Condition, error) {
	tok := p.tokens[p.pos]

	switch tok.typ {
	case tokenNot:
		p.pos++
		inner, err := p.parseSingleCondition()
		if err != nil {
			return nil, err
		}
		return &NotCondition{Condition: inner}, nil

	case tokenAddress, tokenHeader:
		return p.parseHeaderCondition(tok.typ == tokenAddress)

	case tokenSize:
		return p.parseSizeCondition()

	case tokenExists:
		return p.parseExistsCondition()

	case tokenTrue:
		p.pos++
		return &TrueCondition{}, nil

	case tokenFalse:
		p.pos++
		return &FalseCondition{}, nil
	}

	p.pos++
	return nil, nil
}

func (p *Parser) parseHeaderCondition(isAddress bool) (Condition, error) {
	p.pos++ // skip 'header' or 'address'

	var matchType string
	var comparator string
	var addressPart string

	// Parse optional modifiers
	for p.tokens[p.pos].typ == tokenColon {
		p.pos++
		mod := p.tokens[p.pos].val
		p.pos++

		switch mod {
		case "contains", "is", "matches":
			matchType = mod
		case "localpart", "domain", "all":
			addressPart = mod
		case "comparator":
			if p.tokens[p.pos].typ == tokenString {
				comparator = p.tokens[p.pos].val
				p.pos++
			}
		}
	}

	if matchType == "" {
		matchType = "is" // default
	}

	// Parse header names
	var headers []string
	if p.tokens[p.pos].typ == tokenLBracket {
		p.pos++
		for p.tokens[p.pos].typ != tokenRBracket && p.tokens[p.pos].typ != tokenEOF {
			if p.tokens[p.pos].typ == tokenString {
				headers = append(headers, p.tokens[p.pos].val)
			}
			p.pos++
		}
		p.pos++
	} else if p.tokens[p.pos].typ == tokenString {
		headers = append(headers, p.tokens[p.pos].val)
		p.pos++
	}

	// Parse values
	var values []string
	if p.tokens[p.pos].typ == tokenLBracket {
		p.pos++
		for p.tokens[p.pos].typ != tokenRBracket && p.tokens[p.pos].typ != tokenEOF {
			if p.tokens[p.pos].typ == tokenString {
				values = append(values, p.tokens[p.pos].val)
			}
			p.pos++
		}
		p.pos++
	} else if p.tokens[p.pos].typ == tokenString {
		values = append(values, p.tokens[p.pos].val)
		p.pos++
	}

	return &HeaderCondition{
		Headers:     headers,
		Values:      values,
		MatchType:   matchType,
		Comparator:  comparator,
		IsAddress:   isAddress,
		AddressPart: addressPart,
	}, nil
}

func (p *Parser) parseSizeCondition() (Condition, error) {
	p.pos++ // skip 'size'

	var over bool
	if p.tokens[p.pos].typ == tokenColon {
		p.pos++
		if p.tokens[p.pos].typ == tokenOver {
			over = true
		}
		p.pos++
	}

	var size int64
	if p.tokens[p.pos].typ == tokenNumber {
		size = parseSize(p.tokens[p.pos].val)
		p.pos++
	}

	return &SizeCondition{Size: size, Over: over}, nil
}

func (p *Parser) parseExistsCondition() (Condition, error) {
	p.pos++ // skip 'exists'

	var headers []string
	if p.tokens[p.pos].typ == tokenLBracket {
		p.pos++
		for p.tokens[p.pos].typ != tokenRBracket && p.tokens[p.pos].typ != tokenEOF {
			if p.tokens[p.pos].typ == tokenString {
				headers = append(headers, p.tokens[p.pos].val)
			}
			p.pos++
		}
		p.pos++
	} else if p.tokens[p.pos].typ == tokenString {
		headers = append(headers, p.tokens[p.pos].val)
		p.pos++
	}

	return &ExistsCondition{Headers: headers}, nil
}

func (p *Parser) parseAction() (Action, error) {
	tok := p.tokens[p.pos]

	switch tok.typ {
	case tokenKeep:
		p.pos++
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &KeepAction{}, nil

	case tokenFileinto:
		p.pos++
		var folder string
		if p.tokens[p.pos].typ == tokenString {
			folder = p.tokens[p.pos].val
			p.pos++
		}
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &FileIntoAction{Folder: folder}, nil

	case tokenRedirect:
		p.pos++
		var address string
		if p.tokens[p.pos].typ == tokenString {
			address = p.tokens[p.pos].val
			p.pos++
		}
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &RedirectAction{Address: address}, nil

	case tokenDiscard:
		p.pos++
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &DiscardAction{}, nil

	case tokenReject:
		p.pos++
		var message string
		if p.tokens[p.pos].typ == tokenString {
			message = p.tokens[p.pos].val
			p.pos++
		}
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &RejectAction{Message: message}, nil

	case tokenVacation:
		return p.parseVacationAction()

	case tokenStop:
		p.pos++
		if p.tokens[p.pos].typ == tokenSemi {
			p.pos++
		}
		return &StopAction{}, nil

	default:
		p.pos++
		return nil, nil
	}
}

func (p *Parser) parseVacationAction() (Action, error) {
	p.pos++ // skip 'vacation'

	action := &VacationAction{
		Days: 7, // default
	}

	// Parse optional parameters
	for p.tokens[p.pos].typ == tokenColon {
		p.pos++
		param := p.tokens[p.pos].val
		p.pos++

		switch param {
		case "days":
			if p.tokens[p.pos].typ == tokenNumber {
				days, _ := strconv.Atoi(p.tokens[p.pos].val)
				action.Days = days
				p.pos++
			}
		case "subject":
			if p.tokens[p.pos].typ == tokenString {
				action.Subject = p.tokens[p.pos].val
				p.pos++
			}
		case "from":
			if p.tokens[p.pos].typ == tokenString {
				action.From = p.tokens[p.pos].val
				p.pos++
			}
		case "addresses":
			if p.tokens[p.pos].typ == tokenLBracket {
				p.pos++
				for p.tokens[p.pos].typ != tokenRBracket && p.tokens[p.pos].typ != tokenEOF {
					if p.tokens[p.pos].typ == tokenString {
						action.Addresses = append(action.Addresses, p.tokens[p.pos].val)
					}
					p.pos++
				}
				p.pos++
			}
		}
	}

	// Parse message body
	if p.tokens[p.pos].typ == tokenString {
		action.Body = p.tokens[p.pos].val
		p.pos++
	}

	if p.tokens[p.pos].typ == tokenSemi {
		p.pos++
	}

	return action, nil
}

// parseSize parses a size string like "100K" or "1M" into bytes
func parseSize(s string) int64 {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0
	}

	multiplier := int64(1)
	last := s[len(s)-1]

	switch last {
	case 'K', 'k':
		multiplier = 1024
		s = s[:len(s)-1]
	case 'M', 'm':
		multiplier = 1024 * 1024
		s = s[:len(s)-1]
	case 'G', 'g':
		multiplier = 1024 * 1024 * 1024
		s = s[:len(s)-1]
	}

	n, _ := strconv.ParseInt(s, 10, 64)
	return n * multiplier
}
