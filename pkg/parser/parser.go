// Package parser implements a hand-written recursive-descent parser for the
// Sajon (.saj) language.  It consumes the token stream produced by pkg/lexer
// and constructs a typed Abstract Syntax Tree (defined in ast.go) that
// downstream compiler phases operate on.
//
// Design: the Parser maintains a two-token look-ahead window (curToken /
// peekToken) identical in spirit to the Go parser's own approach.  All errors
// are accumulated in the errors slice rather than aborting on the first
// problem, so the caller receives a complete diagnostic list.
package parser

import (
	"fmt"

	"sajon/pkg/lexer"
)

// ── Parser struct ─────────────────────────────────────────────────────────────

// Parser is the stateful recursive-descent parser for Sajon source code.
// All fields are unexported; interaction is via New, ParseProgram, and Errors.
type Parser struct {
	l         *lexer.Lexer // the underlying lexer (token source)
	curToken  lexer.Token  // the token currently being examined
	peekToken lexer.Token  // one-token look-ahead (not yet consumed)
	errors    []string     // accumulated syntax error messages
}

// New constructs a fully-primed Parser for the given Lexer.  Two calls to
// nextToken initialise both curToken and peekToken before ParseProgram runs,
// so no special priming is required at the call site.
func New(l *lexer.Lexer) *Parser {
	p := &Parser{l: l}
	// Advance twice: first call sets peekToken; second shifts it to curToken
	// and loads a new peekToken.  After New returns:
	//   curToken  = input[0]
	//   peekToken = input[1]
	p.nextToken()
	p.nextToken()
	return p
}

// Errors returns the slice of human-readable syntax error messages accumulated
// during parsing.  An empty slice means the source was syntactically valid.
func (p *Parser) Errors() []string {
	return p.errors
}

// ── Token navigation ──────────────────────────────────────────────────────────

// nextToken advances the parser by one position: peekToken becomes curToken
// and the lexer supplies a fresh token for peekToken.
func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

// curTokenIs reports whether the current token is of the given type.
func (p *Parser) curTokenIs(t lexer.TokenType) bool {
	return p.curToken.Type == t
}

// peekTokenIs reports whether the look-ahead token is of the given type.
func (p *Parser) peekTokenIs(t lexer.TokenType) bool {
	return p.peekToken.Type == t
}

// expectPeek advances the parser and returns true when the look-ahead token
// matches the expected type.  If it does not match, a descriptive error is
// appended and false is returned — the caller must handle the nil-statement
// sentinel path.
func (p *Parser) expectPeek(t lexer.TokenType) bool {
	if p.peekTokenIs(t) {
		p.nextToken()
		return true
	}
	p.peekError(t)
	return false
}

// peekError records a "unexpected token" diagnostic in the errors slice.
func (p *Parser) peekError(expected lexer.TokenType) {
	msg := fmt.Sprintf(
		"[syntax error] expected next token to be '%s', got '%s' ('%s') instead",
		expected, p.peekToken.Type, p.peekToken.Value,
	)
	p.errors = append(p.errors, msg)
}

// addError appends a free-form diagnostic message to the errors slice.
func (p *Parser) addError(msg string) {
	p.errors = append(p.errors, "[syntax error] "+msg)
}

// ── Core parse loop ───────────────────────────────────────────────────────────

// ParseProgram is the entry point.  It iterates over top-level tokens,
// delegates to the appropriate statement parser, and collects results into
// the root Program node.  Unrecognised top-level tokens are gracefully skipped
// so that partial programs still yield a meaningful (if incomplete) AST.
func (p *Parser) ParseProgram() *Program {
	program := &Program{
		Statements: []Statement{},
	}

	for !p.curTokenIs(lexer.TokenEOF) {
		stmt := p.parseStatement()
		if stmt != nil {
			program.Statements = append(program.Statements, stmt)
		}
		p.nextToken()
	}

	return program
}

// parseStatement dispatches on the current token type to the matching
// statement parser.  An unknown top-level token returns nil (silently skipped).
func (p *Parser) parseStatement() Statement {
	switch p.curToken.Type {
	case lexer.TokenResource, lexer.TokenDatabase, lexer.TokenWorker,
		lexer.TokenServer, lexer.TokenStorage:
		return p.parseResourceStatement()
	case lexer.TokenEndpoint:
		return p.parseEndpointStatement()
	case lexer.TokenEnv:
		return p.parseEnvStatement()
	default:
		// Unknown top-level token — not a parse error at this stage; simply
		// ignored so that future keyword additions don't break existing files.
		return nil
	}
}

// ── Statement parsers ─────────────────────────────────────────────────────────

// typeQualifierKeywords lists all token types that may appear as an optional
// type-qualifier immediately after the resource name in a two-keyword form:
//
//	RESOURCE <Name> DATABASE { ... }
//
var typeQualifierKeywords = []lexer.TokenType{
	lexer.TokenDatabase,
	lexer.TokenServer,
	lexer.TokenStorage,
	lexer.TokenWorker,
	lexer.TokenResource,
}

// isTypeQualifier reports whether tok is one of the known type-qualifier
// keywords that may follow a resource name.
func isTypeQualifier(tok lexer.Token) bool {
	for _, kw := range typeQualifierKeywords {
		if tok.Type == kw {
			return true
		}
	}
	return false
}

// parseResourceStatement handles RESOURCE, DATABASE, WORKER, SERVER, and
// STORAGE blocks.  It accepts two grammar forms:
//
//	Form A — single keyword:  <Keyword> <Identifier> '{' (<key> ':' <value>)* '}'
//	Form B — two keywords:    RESOURCE <Identifier> <TypeQualifier> '{' (<key> ':' <value>)* '}'
//
// Form B allows declarations such as:
//
//	RESOURCE rentic_prod DATABASE {
//	    provider: "neon"
//	    type: "postgres"
//	}
//
// In Form B the TypeQualifier becomes the block's Kind, so the resulting
// ResourceStatement is indistinguishable from one written in Form A.
func (p *Parser) parseResourceStatement() *ResourceStatement {
	stmt := &ResourceStatement{
		Kind: string(p.curToken.Type), // e.g. "RESOURCE" | "DATABASE" | "WORKER"
	}

	// The block name must be an identifier immediately after the keyword.
	if !p.expectPeek(lexer.TokenIdentifier) {
		return nil
	}
	stmt.Name = p.curToken.Value

	// ── Form B: optional type-qualifier keyword after the name ────────────
	// Example: RESOURCE rentic_prod DATABASE { ... }
	// If the look-ahead is a type-qualifier keyword, consume it and promote
	// it to the block Kind so downstream emitters see "DATABASE" (not
	// "RESOURCE").
	if isTypeQualifier(p.peekToken) {
		p.nextToken() // consume the type-qualifier
		stmt.Kind = string(p.curToken.Type)
	}

	// Opening brace.
	if !p.expectPeek(lexer.TokenLBrace) {
		return nil
	}

	// Parse properties until the matching closing brace or EOF.
	p.nextToken()
	for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
		if p.curTokenIs(lexer.TokenSchema) {
			// Inline SCHEMA block — parse it and attach to the statement.
			if schema := p.parseSchemaBlock(); schema != nil {
				stmt.Schema = schema
			}
		} else if p.curTokenIs(lexer.TokenData) {
			// Inline DATA block — parse seed rows and attach to the statement.
			if data := p.parseDataBlock(); data != nil {
				stmt.Data = data
			}
		} else if prop := p.parseProperty(); prop != nil {
			stmt.Properties = append(stmt.Properties, *prop)
		}
		p.nextToken()
	}

	// Verify we exited the loop on '}' and not on EOF (unclosed block).
	if !p.curTokenIs(lexer.TokenRBrace) {
		p.addError(fmt.Sprintf(
			"missing '}' to close %s block '%s' — unexpected end of input",
			stmt.Kind, stmt.Name,
		))
		return nil
	}

	// Strict property validation: reject any unknown keys before returning.
	p.validateResourceProperties(stmt)

	return stmt
}

// parseSchemaBlock parses an inline schema sub-block inside a RESOURCE or
// DATABASE block.  Grammar:
//
//	SCHEMA '{' ( 'table' ':' STRING | 'fields' ':' '[' ( STRING ',' )* ']' )* '}'
//
// curToken is SCHEMA when this function is called; it returns with curToken
// positioned on the closing '}' of the SCHEMA block so the outer loop's
// trailing nextToken() call naturally lands on the next resource property.
func (p *Parser) parseSchemaBlock() *SchemaBlock {
	schema := &SchemaBlock{}

	// Consume the opening '{' of the SCHEMA block.
	if !p.expectPeek(lexer.TokenLBrace) {
		return nil
	}

	p.nextToken() // advance into the block body
	for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
		if p.curTokenIs(lexer.TokenIdentifier) && p.curToken.Value == "table" {
			// table: "users"
			if !p.expectPeek(lexer.TokenColon) {
				p.nextToken()
				continue
			}
			if !p.expectPeek(lexer.TokenString) {
				p.nextToken()
				continue
			}
			schema.Table = p.curToken.Value

		} else if p.curTokenIs(lexer.TokenIdentifier) && p.curToken.Value == "fields" {
			// fields: ["id:int", "name:string", ...]
			if !p.expectPeek(lexer.TokenColon) {
				p.nextToken()
				continue
			}
			if !p.expectPeek(lexer.TokenLBracket) {
				p.nextToken()
				continue
			}
			// Walk the array elements until ']' or EOF.
			p.nextToken()
			for !p.curTokenIs(lexer.TokenRBracket) && !p.curTokenIs(lexer.TokenEOF) {
				if p.curTokenIs(lexer.TokenString) {
					schema.Fields = append(schema.Fields, p.curToken.Value)
				}
				// skip commas and any unexpected tokens
				p.nextToken()
			}
			// curToken is now ']'
		}
		// Advance to the next property in the SCHEMA block.
		p.nextToken()
	}
	// curToken is now '}' (closing brace of SCHEMA block).
	return schema
}

// parseDataBlock parses an inline DATA sub-block inside a RESOURCE or
// DATABASE block.  Grammar:
//
//	DATA '{'
//	    insert_into: STRING
//	    row: '{' ( IDENTIFIER ':' VALUE )* '}'
//	    ...
//	'}'
//
// curToken is DATA when this function is called; it returns with curToken
// positioned on the closing '}' of the DATA block.
func (p *Parser) parseDataBlock() *DataBlock {
	data := &DataBlock{}

	// Consume the opening '{' of the DATA block.
	if !p.expectPeek(lexer.TokenLBrace) {
		return nil
	}

	p.nextToken() // advance into the block body
	for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
		if p.curTokenIs(lexer.TokenIdentifier) && p.curToken.Value == "insert_into" {
			// insert_into: "table_name"
			if !p.expectPeek(lexer.TokenColon) {
				p.nextToken()
				continue
			}
			if !p.expectPeek(lexer.TokenString) {
				p.nextToken()
				continue
			}
			data.InsertInto = p.curToken.Value

		} else if p.curTokenIs(lexer.TokenIdentifier) && p.curToken.Value == "row" {
			// row: { col: value col: value ... }
			if !p.expectPeek(lexer.TokenColon) {
				p.nextToken()
				continue
			}
			if !p.expectPeek(lexer.TokenLBrace) {
				p.nextToken()
				continue
			}
			// Parse columns inside { ... }
			var row DataRow
			p.nextToken() // advance into the row body
			for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
				if prop := p.parseProperty(); prop != nil {
					row.Columns = append(row.Columns, *prop)
				}
				p.nextToken()
			}
			// curToken is now '}' of the row block.
			data.Rows = append(data.Rows, row)
		}
		// Advance to the next key in the DATA block.
		p.nextToken()
	}
	// curToken is now '}' (closing brace of DATA block).
	return data
}

//
//	ENDPOINT <Method> <Path> '{' (<body-statement>)* '}'
func (p *Parser) parseEndpointStatement() *EndpointStatement {
	stmt := &EndpointStatement{}

	// HTTP method: must be a bare identifier (GET, POST, PUT, DELETE, PATCH…).
	if !p.expectPeek(lexer.TokenIdentifier) {
		return nil
	}
	stmt.Method = p.curToken.Value

	// Route path: must be a string literal (e.g. "/signup").
	if !p.expectPeek(lexer.TokenString) {
		return nil
	}
	stmt.Path = p.curToken.Value

	// Opening brace.
	if !p.expectPeek(lexer.TokenLBrace) {
		return nil
	}

	// Parse body statements until the closing brace or EOF.
	p.nextToken()
	for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
		if bodyStmt := p.parseBodyStatement(); bodyStmt != nil {
			stmt.Body = append(stmt.Body, bodyStmt)
		}
		p.nextToken()
	}

	// Guard against unclosed endpoint blocks.
	if !p.curTokenIs(lexer.TokenRBrace) {
		p.addError(fmt.Sprintf(
			"missing '}' to close ENDPOINT block '%s \"%s\"' — unexpected end of input",
			stmt.Method, stmt.Path,
		))
		return nil
	}

	return stmt
}

// parseBodyStatement handles statements that may appear inside a block body.
// Currently only RETURN is supported; additional body-level constructs (IF,
// assignment, expression statements) can be added here incrementally.
func (p *Parser) parseBodyStatement() Statement {
	switch p.curToken.Type {
	case lexer.TokenReturn:
		return p.parseReturnStatement()
	default:
		return nil
	}
}

// parseReturnStatement handles:
//
//	RETURN <value>
//
// where <value> is any single-token literal or identifier.
func (p *Parser) parseReturnStatement() *ReturnStatement {
	// Advance past the RETURN keyword to the value token.
	p.nextToken()

	// FIX #2: Check for both EOF and RBrace.
	// Previously only EOF was checked, so a bare "RETURN" followed by "}"
	// would silently treat "}" as the return value, causing the ENDPOINT
	// block to be unclosed and giving a misleading "missing '}'" error.
	// Now we give the developer a clear, actionable message.
	if p.curTokenIs(lexer.TokenEOF) || p.curTokenIs(lexer.TokenRBrace) {
		p.addError(fmt.Sprintf(
			"missing value after RETURN keyword — expected a string, number, or identifier (got '%s')",
			p.curToken.Value,
		))
		return nil
	}

	return &ReturnStatement{Value: p.curToken.Value}
}


// parseProperty handles a single key-value pair inside a block:
//
//	<identifier> ':' <value>
//
// where <value> is a string literal, number literal, bare identifier,
// or a negative number literal (e.g. -1).
func (p *Parser) parseProperty() *Property {
	// Key must be an identifier.
	if !p.curTokenIs(lexer.TokenIdentifier) {
		return nil
	}
	key := p.curToken.Value

	// Colon separator.
	if !p.expectPeek(lexer.TokenColon) {
		return nil
	}

	// Value: accept string, number, identifier, or negative number.
	p.nextToken()
	switch p.curToken.Type {
	case lexer.TokenString, lexer.TokenNumber, lexer.TokenIdentifier:
		return &Property{Key: key, Value: p.curToken.Value}

	case lexer.TokenIllegal:
		// BUG #10 FIX: Support negative number literals (e.g. version: -1).
		// The lexer emits '-' as ILLEGAL since it is not a letter or digit;
		// if the next token is a NUMBER we treat the pair as a negative literal.
		if p.curToken.Value == "-" && p.peekTokenIs(lexer.TokenNumber) {
			p.nextToken() // consume the number
			return &Property{Key: key, Value: "-" + p.curToken.Value}
		}
		// BUG #9 FIX: Surface unclosed string as a clear parser error.
		if len(p.curToken.Value) > 16 && p.curToken.Value[:16] == "unclosed string:" {
			p.addError(fmt.Sprintf(
				"unterminated string literal for property '%s' — add a closing '\"'",
				key,
			))
			return nil
		}
		p.addError(fmt.Sprintf(
			"invalid value for property '%s': got token '%s' ('%s')",
			key, p.curToken.Type, p.curToken.Value,
		))
		return nil

	default:
		p.addError(fmt.Sprintf(
			"invalid value for property '%s': got token '%s' ('%s')",
			key, p.curToken.Type, p.curToken.Value,
		))
		return nil
	}
}

// parseEnvStatement handles ENV blocks:
//
//	ENV <Name> '{' (<KEY> ':' <value>)* '}'
//
// The parsed key-value pairs are stored as []Property in EnvStatement.Vars.
// The emitter injects all Vars into the environment: section of every service.
func (p *Parser) parseEnvStatement() *EnvStatement {
	stmt := &EnvStatement{}

	// Block label (e.g. "production") must follow the ENV keyword.
	if !p.expectPeek(lexer.TokenIdentifier) {
		return nil
	}
	stmt.Name = p.curToken.Value

	// Opening brace.
	if !p.expectPeek(lexer.TokenLBrace) {
		return nil
	}

	// Parse KEY: "value" pairs until closing brace or EOF.
	p.nextToken()
	for !p.curTokenIs(lexer.TokenRBrace) && !p.curTokenIs(lexer.TokenEOF) {
		if prop := p.parseProperty(); prop != nil {
			stmt.Vars = append(stmt.Vars, *prop)
		}
		p.nextToken()
	}

	// Guard against unclosed ENV block.
	if !p.curTokenIs(lexer.TokenRBrace) {
		p.addError(fmt.Sprintf(
			"missing '}' to close ENV block '%s' — unexpected end of input",
			stmt.Name,
		))
		return nil
	}

	return stmt
}

// ── Strict resource property validation ───────────────────────────────────────

// allowedResourceProps defines the complete set of recognised property keys for
// each block Kind, optionally specialised by provider value.
//
// Layout of the outer map:
//
//	"KIND" → { "providerOrWildcard" → set-of-allowed-keys }
//
// The lookup order is:
//  1. exact match on (kind, provider)  — e.g. ("RESOURCE", "supabase")
//  2. wildcard match on (kind, "*")    — applies to every provider for that kind
//
// A key present in the wildcard set is always allowed regardless of provider.
var allowedResourceProps = map[string]map[string]map[string]bool{
	// ── RESOURCE / DATABASE blocks ─────────────────────────────────────────
	"RESOURCE": {
		// Common keys available for any provider.
		"*": {
			"provider": true,
			"engine":   true,
			"type":     true,
			"region":   true,
			"version":  true,
		},
		// Supabase-specific extras.
		"supabase": {
			"db_password": true,
		},
		// Neon-specific extras.
		"neon": {},
		// Self-hosted postgres extras.
		"postgres": {},
		// AWS RDS extras.
		"aws": {
			"size": true,
		},
		// BigQuery extras.
		"bigquery": {
			"dataset": true,
		},
	},
	// DATABASE blocks share the same rules as RESOURCE.
	"DATABASE": {
		"*": {
			"provider": true,
			"engine":   true,
			"type":     true,
			"region":   true,
			"version":  true,
		},
		"supabase": {
			"db_password": true,
		},
		"neon": {},
		"postgres": {},
		"aws": {
			"size": true,
		},
		"bigquery": {
			"dataset": true,
		},
	},
	// ── WORKER blocks ──────────────────────────────────────────────────────
	"WORKER": {
		"*": {
			"provider":    true,
			"queue":       true,
			"concurrency": true,
			"region":      true,
		},
	},
	// ── SERVER blocks (EC2 / VM) ───────────────────────────────────────────
	"SERVER": {
		"*": {
			"provider":      true,
			"region":        true,
			"instance_type": true,
			"ami":           true,
		},
	},
	// ── STORAGE blocks (S3 / GCS) ─────────────────────────────────────────
	"STORAGE": {
		"*": {
			"provider":    true,
			"region":      true,
			"bucket_name": true,
			"bucket":      true,
		},
	},
}

// validateResourceProperties checks every parsed property key in stmt against
// the compile-time allowlist.  Unknown keys produce a hard parser error so
// the compiler stops with a clear diagnostic instead of silently ignoring
// unrecognised fields.
//
// Called immediately before parseResourceStatement returns, after the full
// body has been parsed and stmt.Properties is populated.
func (p *Parser) validateResourceProperties(stmt *ResourceStatement) {
	// Look up the per-kind rule table.
	kindsMap, ok := allowedResourceProps[stmt.Kind]
	if !ok {
		// Unknown Kind — no validation rules; pass through (forward-compat).
		return
	}

	// Determine the provider value (may be empty).
	provider := ""
	for _, prop := range stmt.Properties {
		if prop.Key == "provider" {
			provider = prop.Value
			break
		}
	}

	// Build the effective allowed set: wildcard keys + provider-specific keys.
	allowed := make(map[string]bool)
	for k := range kindsMap["*"] {
		allowed[k] = true
	}
	if provider != "" {
		for k := range kindsMap[provider] {
			allowed[k] = true
		}
	}

	// Build a human-readable sorted list of valid keys for error messages.
	validList := make([]string, 0, len(allowed))
	for k := range allowed {
		validList = append(validList, k)
	}
	// Simple insertion sort (short slice — no need for sort package).
	for i := 1; i < len(validList); i++ {
		for j := i; j > 0 && validList[j] < validList[j-1]; j-- {
			validList[j], validList[j-1] = validList[j-1], validList[j]
		}
	}
	validStr := ""
	for i, k := range validList {
		if i > 0 {
			validStr += ", "
		}
		validStr += k
	}

	// Check every parsed property key.
	for _, prop := range stmt.Properties {
		if !allowed[prop.Key] {
			p.addError(fmt.Sprintf(
				"Unknown property '%s' in %s block '%s' — valid properties are: %s",
				prop.Key, stmt.Kind, stmt.Name, validStr,
			))
		}
	}
}
