package sieve

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Limits to prevent DoS attacks
const (
	maxScriptSize      = 1024 * 1024 // 1MB max script size
	maxTokens          = 100000      // Maximum number of tokens
	maxStringLength    = 10000       // Maximum string literal length
	maxArraySize       = 1000        // Maximum array size
	maxParseDepth      = 100         // Maximum parsing recursion depth
	maxConditionDepth  = 50          // Maximum condition nesting
	maxVacationDays    = 365         // Maximum vacation days
	maxSizeValue       = 1024 * 1024 * 1024 * 10 // 10GB max size value
)

var (
	ErrScriptTooLarge    = errors.New("sieve script exceeds maximum size")
	ErrTooManyTokens     = errors.New("sieve script has too many tokens")
	ErrStringTooLong     = errors.New("string literal exceeds maximum length")
	ErrArrayTooLarge     = errors.New("array exceeds maximum size")
	ErrNestingTooDeep    = errors.New("nesting depth exceeds maximum")
	ErrInvalidInput      = errors.New("invalid input in script")
	ErrUnterminatedString = errors.New("unterminated string literal")
	ErrInvalidSize       = errors.New("invalid size value")
)

// Parser parses Sieve scripts into executable rules
type Parser struct {
	input  string
	pos    int
	tokens []token
	depth  int // Current parsing depth
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
	// Validate script size to prevent DoS
	if len(script) > maxScriptSize {
		return nil, ErrScriptTooLarge
	}

	p := &Parser{input: script}
	if err := p.tokenize(); err != nil {
		return nil, err
	}
	return p.parse()
}

// peek returns the current token without advancing, or nil if out of bounds
func (p *Parser) peek() *token {
	if p.pos >= len(p.tokens) {
		return nil
	}
	return &p.tokens[p.pos]
}

// current returns the current token or EOF token if out of bounds
func (p *Parser) current() token {
	if p.pos >= len(p.tokens) {
		return token{typ: tokenEOF, val: ""}
	}
	return p.tokens[p.pos]
}

// advance moves to the next token
func (p *Parser) advance() {
	if p.pos < len(p.tokens) {
		p.pos++
	}
}

// expect checks if current token matches expected type and advances
func (p *Parser) expect(expected tokenType) error {
	if p.pos >= len(p.tokens) {
		return fmt.Errorf("unexpected end of script, expected %v", expected)
	}
	if p.tokens[p.pos].typ != expected {
		return fmt.Errorf("expected %v, got %v", expected, p.tokens[p.pos].typ)
	}
	p.pos++
	return nil
}

func (p *Parser) tokenize() error {
	// Simple tokenizer for Sieve subset
	s := p.input

	// Remove comments with safe regex
	commentRegex := regexp.MustCompile(`#[^\n]*`)
	s = commentRegex.ReplaceAllString(s, "")

	// Remove block comments with safe regex
	blockCommentRegex := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	s = blockCommentRegex.ReplaceAllString(s, "")

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
	iterations := 0
	maxIterations := len(s) + 1000 // Safety limit

	for i < len(s) {
		// Prevent infinite loops
		iterations++
		if iterations > maxIterations {
			return ErrInvalidInput
		}

		// Check token limit
		if len(p.tokens) >= maxTokens {
			return ErrTooManyTokens
		}

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
			stringIterations := 0
			for i < len(s) && s[i] != '"' {
				stringIterations++
				if stringIterations > maxStringLength {
					return ErrStringTooLong
				}
				if s[i] == '\\' && i+1 < len(s) {
					i++ // Skip escaped char
				}
				i++
			}
			if i >= len(s) {
				return ErrUnterminatedString
			}
			val := s[start:i]
			if len(val) > maxStringLength {
				return ErrStringTooLong
			}
			val = strings.ReplaceAll(val, `\"`, `"`)
			val = strings.ReplaceAll(val, `\\`, `\`)
			p.tokens = append(p.tokens, token{tokenString, val})
			i++ // Skip closing quote
			continue
		}

		// Number
		if ch >= '0' && ch <= '9' {
			start := i
			numLen := 0
			for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == 'K' || s[i] == 'M' || s[i] == 'G') {
				numLen++
				if numLen > 100 {
					return ErrInvalidInput
				}
				i++
			}
			p.tokens = append(p.tokens, token{tokenNumber, s[start:i]})
			continue
		}

		// Keyword or identifier
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_' {
			start := i
			identLen := 0
			for i < len(s) && ((s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') || (s[i] >= '0' && s[i] <= '9') || s[i] == '_') {
				identLen++
				if identLen > 1000 {
					return ErrInvalidInput
				}
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
	iterations := 0

	for p.pos < len(p.tokens) {
		// Prevent infinite loops
		iterations++
		if iterations > maxTokens {
			return nil, ErrInvalidInput
		}

		tok := p.current()
		if tok.typ == tokenEOF {
			break
		}

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
			if rule != nil {
				script.Rules = append(script.Rules, *rule)
			}

		default:
			p.advance()
		}
	}

	return script, nil
}

func (p *Parser) parseRequire() ([]string, error) {
	p.advance() // skip 'require'

	var reqs []string
	tok := p.current()

	if tok.typ == tokenLBracket {
		// Array of requirements
		p.advance()
		arrayCount := 0
		for {
			tok = p.current()
			if tok.typ == tokenRBracket || tok.typ == tokenEOF {
				break
			}
			arrayCount++
			if arrayCount > maxArraySize {
				return nil, ErrArrayTooLarge
			}
			if tok.typ == tokenString {
				reqs = append(reqs, tok.val)
			}
			p.advance()
		}
		if tok.typ == tokenRBracket {
			p.advance() // skip ']'
		}
	} else if tok.typ == tokenString {
		reqs = append(reqs, tok.val)
		p.advance()
	}

	tok = p.current()
	if tok.typ == tokenSemi {
		p.advance()
	}

	return reqs, nil
}

func (p *Parser) parseRule() (*Rule, error) {
	// Check parsing depth
	p.depth++
	if p.depth > maxParseDepth {
		return nil, ErrNestingTooDeep
	}
	defer func() { p.depth-- }()

	rule := &Rule{}
	p.advance() // skip 'if'

	// Parse condition
	cond, allOf, err := p.parseCondition()
	if err != nil {
		return nil, err
	}
	rule.Conditions = cond
	rule.AllOf = allOf

	// Parse actions block
	tok := p.current()
	if tok.typ != tokenLBrace {
		return nil, fmt.Errorf("expected '{' after condition, got %v", tok.typ)
	}
	p.advance()

	actionCount := 0
	for {
		tok = p.current()
		if tok.typ == tokenRBrace || tok.typ == tokenEOF {
			break
		}
		actionCount++
		if actionCount > maxArraySize {
			return nil, ErrArrayTooLarge
		}
		action, err := p.parseAction()
		if err != nil {
			return nil, err
		}
		if action != nil {
			rule.Actions = append(rule.Actions, action)
		}
	}
	if tok.typ == tokenRBrace {
		p.advance() // skip '}'
	}

	return rule, nil
}

func (p *Parser) parseCondition() ([]Condition, bool, error) {
	// Check condition depth
	p.depth++
	if p.depth > maxConditionDepth {
		return nil, false, ErrNestingTooDeep
	}
	defer func() { p.depth-- }()

	var conditions []Condition
	allOf := true

	tok := p.current()

	switch tok.typ {
	case tokenAllof:
		p.advance()
		allOf = true
		tok = p.current()
		if tok.typ == tokenLParen {
			p.advance()
			condCount := 0
			for {
				tok = p.current()
				if tok.typ == tokenRParen || tok.typ == tokenEOF {
					break
				}
				condCount++
				if condCount > maxArraySize {
					return nil, false, ErrArrayTooLarge
				}
				cond, err := p.parseSingleCondition()
				if err != nil {
					return nil, false, err
				}
				if cond != nil {
					conditions = append(conditions, cond)
				}
				tok = p.current()
				if tok.typ == tokenComma {
					p.advance()
				}
			}
			if tok.typ == tokenRParen {
				p.advance() // skip ')'
			}
		}

	case tokenAnyof:
		p.advance()
		allOf = false
		tok = p.current()
		if tok.typ == tokenLParen {
			p.advance()
			condCount := 0
			for {
				tok = p.current()
				if tok.typ == tokenRParen || tok.typ == tokenEOF {
					break
				}
				condCount++
				if condCount > maxArraySize {
					return nil, false, ErrArrayTooLarge
				}
				cond, err := p.parseSingleCondition()
				if err != nil {
					return nil, false, err
				}
				if cond != nil {
					conditions = append(conditions, cond)
				}
				tok = p.current()
				if tok.typ == tokenComma {
					p.advance()
				}
			}
			if tok.typ == tokenRParen {
				p.advance() // skip ')'
			}
		}

	case tokenTrue:
		p.advance()
		conditions = append(conditions, &TrueCondition{})

	case tokenFalse:
		p.advance()
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
	// Check recursion depth for NOT conditions
	p.depth++
	if p.depth > maxConditionDepth {
		return nil, ErrNestingTooDeep
	}
	defer func() { p.depth-- }()

	tok := p.current()

	switch tok.typ {
	case tokenNot:
		p.advance()
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
		p.advance()
		return &TrueCondition{}, nil

	case tokenFalse:
		p.advance()
		return &FalseCondition{}, nil
	}

	p.advance()
	return nil, nil
}

func (p *Parser) parseHeaderCondition(isAddress bool) (Condition, error) {
	p.advance() // skip 'header' or 'address'

	var matchType string
	var comparator string
	var addressPart string

	// Parse optional modifiers
	modCount := 0
	for p.current().typ == tokenColon {
		modCount++
		if modCount > 10 {
			return nil, ErrInvalidInput
		}
		p.advance()
		tok := p.current()
		mod := tok.val
		p.advance()

		switch mod {
		case "contains", "is", "matches":
			matchType = mod
		case "localpart", "domain", "all":
			addressPart = mod
		case "comparator":
			tok = p.current()
			if tok.typ == tokenString {
				comparator = tok.val
				p.advance()
			}
		}
	}

	if matchType == "" {
		matchType = "is" // default
	}

	// Parse header names
	var headers []string
	tok := p.current()
	if tok.typ == tokenLBracket {
		p.advance()
		arrayCount := 0
		for {
			tok = p.current()
			if tok.typ == tokenRBracket || tok.typ == tokenEOF {
				break
			}
			arrayCount++
			if arrayCount > maxArraySize {
				return nil, ErrArrayTooLarge
			}
			if tok.typ == tokenString {
				if len(tok.val) > maxStringLength {
					return nil, ErrStringTooLong
				}
				headers = append(headers, tok.val)
			}
			p.advance()
		}
		if tok.typ == tokenRBracket {
			p.advance()
		}
	} else if tok.typ == tokenString {
		if len(tok.val) > maxStringLength {
			return nil, ErrStringTooLong
		}
		headers = append(headers, tok.val)
		p.advance()
	}

	// Parse values
	var values []string
	tok = p.current()
	if tok.typ == tokenLBracket {
		p.advance()
		arrayCount := 0
		for {
			tok = p.current()
			if tok.typ == tokenRBracket || tok.typ == tokenEOF {
				break
			}
			arrayCount++
			if arrayCount > maxArraySize {
				return nil, ErrArrayTooLarge
			}
			if tok.typ == tokenString {
				if len(tok.val) > maxStringLength {
					return nil, ErrStringTooLong
				}
				values = append(values, tok.val)
			}
			p.advance()
		}
		if tok.typ == tokenRBracket {
			p.advance()
		}
	} else if tok.typ == tokenString {
		if len(tok.val) > maxStringLength {
			return nil, ErrStringTooLong
		}
		values = append(values, tok.val)
		p.advance()
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
	p.advance() // skip 'size'

	var over bool
	tok := p.current()
	if tok.typ == tokenColon {
		p.advance()
		tok = p.current()
		if tok.typ == tokenOver {
			over = true
		}
		p.advance()
	}

	var size int64
	tok = p.current()
	if tok.typ == tokenNumber {
		var err error
		size, err = parseSize(tok.val)
		if err != nil {
			return nil, err
		}
		p.advance()
	}

	return &SizeCondition{Size: size, Over: over}, nil
}

func (p *Parser) parseExistsCondition() (Condition, error) {
	p.advance() // skip 'exists'

	var headers []string
	tok := p.current()
	if tok.typ == tokenLBracket {
		p.advance()
		arrayCount := 0
		for {
			tok = p.current()
			if tok.typ == tokenRBracket || tok.typ == tokenEOF {
				break
			}
			arrayCount++
			if arrayCount > maxArraySize {
				return nil, ErrArrayTooLarge
			}
			if tok.typ == tokenString {
				if len(tok.val) > maxStringLength {
					return nil, ErrStringTooLong
				}
				headers = append(headers, tok.val)
			}
			p.advance()
		}
		if tok.typ == tokenRBracket {
			p.advance()
		}
	} else if tok.typ == tokenString {
		if len(tok.val) > maxStringLength {
			return nil, ErrStringTooLong
		}
		headers = append(headers, tok.val)
		p.advance()
	}

	return &ExistsCondition{Headers: headers}, nil
}

func (p *Parser) parseAction() (Action, error) {
	tok := p.current()

	switch tok.typ {
	case tokenKeep:
		p.advance()
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		return &KeepAction{}, nil

	case tokenFileinto:
		p.advance()
		var folder string
		tok = p.current()
		if tok.typ == tokenString {
			if len(tok.val) > maxStringLength {
				return nil, ErrStringTooLong
			}
			folder = tok.val
			p.advance()
		}
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		if folder == "" {
			return nil, fmt.Errorf("fileinto requires a folder name")
		}
		return &FileIntoAction{Folder: folder}, nil

	case tokenRedirect:
		p.advance()
		var address string
		tok = p.current()
		if tok.typ == tokenString {
			if len(tok.val) > maxStringLength {
				return nil, ErrStringTooLong
			}
			address = tok.val
			p.advance()
		}
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		if address == "" {
			return nil, fmt.Errorf("redirect requires an address")
		}
		return &RedirectAction{Address: address}, nil

	case tokenDiscard:
		p.advance()
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		return &DiscardAction{}, nil

	case tokenReject:
		p.advance()
		var message string
		tok = p.current()
		if tok.typ == tokenString {
			if len(tok.val) > maxStringLength {
				return nil, ErrStringTooLong
			}
			message = tok.val
			p.advance()
		}
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		return &RejectAction{Message: message}, nil

	case tokenVacation:
		return p.parseVacationAction()

	case tokenStop:
		p.advance()
		tok = p.current()
		if tok.typ == tokenSemi {
			p.advance()
		}
		return &StopAction{}, nil

	default:
		p.advance()
		return nil, nil
	}
}

func (p *Parser) parseVacationAction() (Action, error) {
	p.advance() // skip 'vacation'

	action := &VacationAction{
		Days: 7, // default
	}

	// Parse optional parameters
	paramCount := 0
	for p.current().typ == tokenColon {
		paramCount++
		if paramCount > 20 {
			return nil, ErrInvalidInput
		}
		p.advance()
		tok := p.current()
		param := tok.val
		p.advance()

		switch param {
		case "days":
			tok = p.current()
			if tok.typ == tokenNumber {
				days, err := strconv.Atoi(tok.val)
				if err != nil {
					return nil, fmt.Errorf("invalid vacation days value: %w", err)
				}
				if days < 0 || days > maxVacationDays {
					return nil, fmt.Errorf("vacation days must be between 0 and %d", maxVacationDays)
				}
				action.Days = days
				p.advance()
			}
		case "subject":
			tok = p.current()
			if tok.typ == tokenString {
				if len(tok.val) > maxStringLength {
					return nil, ErrStringTooLong
				}
				action.Subject = tok.val
				p.advance()
			}
		case "from":
			tok = p.current()
			if tok.typ == tokenString {
				if len(tok.val) > maxStringLength {
					return nil, ErrStringTooLong
				}
				action.From = tok.val
				p.advance()
			}
		case "addresses":
			tok = p.current()
			if tok.typ == tokenLBracket {
				p.advance()
				arrayCount := 0
				for {
					tok = p.current()
					if tok.typ == tokenRBracket || tok.typ == tokenEOF {
						break
					}
					arrayCount++
					if arrayCount > maxArraySize {
						return nil, ErrArrayTooLarge
					}
					if tok.typ == tokenString {
						if len(tok.val) > maxStringLength {
							return nil, ErrStringTooLong
						}
						action.Addresses = append(action.Addresses, tok.val)
					}
					p.advance()
				}
				if tok.typ == tokenRBracket {
					p.advance()
				}
			}
		}
	}

	// Parse message body
	tok := p.current()
	if tok.typ == tokenString {
		if len(tok.val) > maxStringLength {
			return nil, ErrStringTooLong
		}
		action.Body = tok.val
		p.advance()
	}

	tok = p.current()
	if tok.typ == tokenSemi {
		p.advance()
	}

	return action, nil
}

// parseSize parses a size string like "100K" or "1M" into bytes
func parseSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if len(s) == 0 {
		return 0, nil
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

	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size value: %w", err)
	}

	// Check for overflow
	if n < 0 {
		return 0, ErrInvalidSize
	}
	if multiplier > 1 && n > maxSizeValue/multiplier {
		return 0, ErrInvalidSize
	}

	result := n * multiplier
	if result < 0 || result > maxSizeValue {
		return 0, ErrInvalidSize
	}

	return result, nil
}
