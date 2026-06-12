// Package lexer implements the tokenizer for the Sajon (.saj) programming language.
// It converts raw source text into a flat stream of strongly-typed tokens
// consumed by downstream parsing stages.
package lexer

// TokenType is the canonical string identifier for every token class.
// Using a named string type instead of iota gives self-documenting token
// streams and trivially human-readable debug output.
type TokenType string

// Token is the atomic unit produced by the lexer.
// It carries both the semantic type and the original source text so that
// later compiler stages can reconstruct error messages with full fidelity.
type Token struct {
	Type  TokenType
	Value string
}

// ── Keywords ──────────────────────────────────────────────────────────────────
// Reserved words that drive infrastructure block and control-flow semantics.
const (
	// TokenResource declares a cloud resource block (e.g. a managed database).
	TokenResource TokenType = "RESOURCE"
	// TokenDatabase declares a dedicated database configuration block.
	TokenDatabase TokenType = "DATABASE"
	// TokenEndpoint declares an HTTP endpoint handler block.
	TokenEndpoint TokenType = "ENDPOINT"
	// TokenWorker declares an asynchronous background worker block.
	TokenWorker TokenType = "WORKER"
	// TokenIf introduces a conditional expression inside a block.
	TokenIf TokenType = "IF"
	// TokenReturn emits a response or value from an endpoint / worker.
	TokenReturn TokenType = "RETURN"
	// TokenEnv declares a named block of environment variable key-value pairs
	// that the emitter injects into every generated service definition.
	TokenEnv TokenType = "ENV"
	// TokenServer declares a compute/server resource block (maps to EC2, VMs, etc.).
	TokenServer TokenType = "SERVER"
	// TokenStorage declares an object/blob storage resource block (maps to S3, GCS, etc.).
	TokenStorage TokenType = "STORAGE"
	// TokenSchema declares an inline database table schema inside a DATABASE/RESOURCE block.
	// Syntax: SCHEMA { table: "name"  fields: ["col:type", ...] }
	TokenSchema TokenType = "SCHEMA"
	// TokenData declares an inline data-seeding block inside a DATABASE/RESOURCE block.
	// Syntax: DATA { insert_into: "table"  row: { key: value ... } }
	TokenData TokenType = "DATA"
)

// ── Symbols ───────────────────────────────────────────────────────────────────
// Single-character punctuation tokens used to delimit and annotate blocks.
const (
	// TokenLBrace represents the '{' block-open delimiter.
	TokenLBrace TokenType = "LBRACE"
	// TokenRBrace represents the '}' block-close delimiter.
	TokenRBrace TokenType = "RBRACE"
	// TokenColon represents ':' used in key–value property pairs.
	TokenColon TokenType = "COLON"
	// TokenComma represents ',' used to separate list elements.
	TokenComma TokenType = "COMMA"
	// TokenAssign represents '=' used in variable assignment statements.
	TokenAssign TokenType = "ASSIGN"
	// TokenLBracket represents '[' used to open an array literal.
	TokenLBracket TokenType = "LBRACKET"
	// TokenRBracket represents ']' used to close an array literal.
	TokenRBracket TokenType = "RBRACKET"
)

// ── Literals ──────────────────────────────────────────────────────────────────
// Variable-length tokens whose value carries user-defined content.
const (
	// TokenIdentifier is a user-defined name (variable, label, route, etc.).
	TokenIdentifier TokenType = "IDENTIFIER"
	// TokenString is a double-quoted string literal (value excludes quotes).
	TokenString TokenType = "STRING"
	// TokenNumber is an unquoted integer or floating-point numeric literal.
	TokenNumber TokenType = "NUMBER"
)

// ── Special ───────────────────────────────────────────────────────────────────
// Sentinel tokens that signal lexer state to the calling parser.
const (
	// TokenEOF signals the end of the input stream; no more tokens follow.
	TokenEOF TokenType = "EOF"
	// TokenIllegal wraps any character the lexer cannot classify, enabling
	// graceful error reporting instead of a hard panic.
	TokenIllegal TokenType = "ILLEGAL"
)

// keywords maps every reserved word to its canonical TokenType.
// Centralising the table here means adding a new keyword requires a single
// constant declaration above and one entry in this map — nothing else.
var keywords = map[string]TokenType{
	"RESOURCE": TokenResource,
	"DATABASE": TokenDatabase,
	"ENDPOINT": TokenEndpoint,
	"WORKER":   TokenWorker,
	"IF":       TokenIf,
	"RETURN":   TokenReturn,
	"ENV":      TokenEnv,
	"SERVER":   TokenServer,
	"STORAGE":  TokenStorage,
	"SCHEMA":   TokenSchema,
	"DATA":     TokenData,
}

// LookupIdentifier resolves a raw identifier string to its TokenType.
// If the string is a reserved keyword the keyword type is returned;
// otherwise TokenIdentifier is returned to denote a user-defined name.
func LookupIdentifier(ident string) TokenType {
	if tok, ok := keywords[ident]; ok {
		return tok
	}
	return TokenIdentifier
}
