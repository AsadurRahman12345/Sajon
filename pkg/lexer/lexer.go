// Package lexer implements the core tokeniser for the Sajon (.saj) language.
// It operates as a single-pass, byte-oriented scanner: it reads one byte at a
// time through a sliding window of two positions (position / readPosition) so
// that one-character look-ahead is always available without back-tracking.
package lexer

// Lexer is the stateful scanner that converts a raw Sajon source string into a
// sequential stream of Tokens.  All fields are unexported; callers interact
// exclusively through New and NextToken.
type Lexer struct {
	input       string // full source text (immutable after construction)
	position    int    // index of the byte currently under examination (ch)
	readPosition int    // index of the next byte to be consumed (look-ahead)
	ch          byte   // byte currently being examined; 0 signals EOF
}

// New constructs a fully initialised Lexer for the given input string and
// advances the scanner to the very first character so that NextToken can be
// called immediately without an additional priming step.
func New(input string) *Lexer {
	l := &Lexer{input: input}
	l.readChar() // prime: populate ch with input[0] and advance readPosition
	return l
}

// ── Internal helpers ──────────────────────────────────────────────────────────

// readChar advances the scanner by one byte.  When the end of input is reached
// ch is set to 0 (the null byte) which acts as the sentinel for EOF throughout
// the lexer; this mirrors the classic Aho/Ullman design so that every code path
// can test "ch == 0" instead of a separate boolean flag.
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0 // EOF sentinel
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
}

// peekChar returns the next byte without consuming it.  Used to distinguish
// two-character tokens (future extension) or to validate sequence structure.
func (l *Lexer) peekChar() byte {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// skipWhitespace consumes all horizontal and vertical whitespace characters.
// Sajon treats whitespace as purely cosmetic — it has no indentation semantics.
func (l *Lexer) skipWhitespace() {
	for l.ch == ' ' || l.ch == '\t' || l.ch == '\n' || l.ch == '\r' {
		l.readChar()
	}
}

// skipLineComment consumes every byte on the current line after a '#' has been
// detected, leaving the scanner positioned on the newline so that the next
// skipWhitespace call will cross it.
func (l *Lexer) skipLineComment() {
	for l.ch != '\n' && l.ch != 0 {
		l.readChar()
	}
}

// readIdentifier consumes a contiguous run of letters, digits, and underscores
// starting at the current position.  Sajon identifiers follow the conventional
// [A-Za-z_][A-Za-z0-9_]* grammar; the first byte has already been validated
// by the caller.
func (l *Lexer) readIdentifier() string {
	start := l.position
	for isLetter(l.ch) || isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.position]
}

// readNumber consumes a contiguous run of digit characters.  The lexer
// intentionally remains simple at this stage: full floating-point literals
// (including exponents) can be added here later without touching any other
// module.
func (l *Lexer) readNumber() string {
	start := l.position
	for isDigit(l.ch) {
		l.readChar()
	}
	return l.input[start:l.position]
}

// readString consumes all bytes between the opening and closing double-quote
// characters and returns the content without the surrounding quotes.
// The second return value is false when the input ends without a closing quote
// (BUG #9 FIX: previously this silently returned a truncated string, which
// caused the parser to see a valid STRING token for broken input).
func (l *Lexer) readString() (string, bool) {
	// position is currently on the opening '"'; advance past it.
	l.readChar()
	start := l.position
	for l.ch != '"' && l.ch != 0 {
		l.readChar()
	}
	str := l.input[start:l.position]
	if l.ch == 0 {
		// Reached EOF without a closing quote.
		return str, false
	}
	l.readChar() // consume the closing '"'
	return str, true
}

// ── Core tokenisation ─────────────────────────────────────────────────────────

// NextToken skips whitespace and comments then classifies the current character,
// returning the corresponding Token.  The method is the only public API the
// parser needs — repeatedly calling NextToken drives the entire lexer state
// machine forward until TokenEOF is returned.
func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	// Comment lines begin with '#'; skip the entire line and recurse so that
	// the caller always receives a meaningful token, never a comment fragment.
	if l.ch == '#' {
		l.skipLineComment()
		return l.NextToken()
	}

	switch l.ch {
	// ── Single-character symbols ──────────────────────────────────────────
	case '{':
		tok := Token{Type: TokenLBrace, Value: string(l.ch)}
		l.readChar()
		return tok

	case '}':
		tok := Token{Type: TokenRBrace, Value: string(l.ch)}
		l.readChar()
		return tok

	case ':':
		tok := Token{Type: TokenColon, Value: string(l.ch)}
		l.readChar()
		return tok

	case ',':
		tok := Token{Type: TokenComma, Value: string(l.ch)}
		l.readChar()
		return tok

	case '=':
		tok := Token{Type: TokenAssign, Value: string(l.ch)}
		l.readChar()
		return tok

	case '[':
		tok := Token{Type: TokenLBracket, Value: string(l.ch)}
		l.readChar()
		return tok

	case ']':
		tok := Token{Type: TokenRBracket, Value: string(l.ch)}
		l.readChar()
		return tok

	// ── String literals ───────────────────────────────────────────────────
	case '"':
		str, ok := l.readString()
		if !ok {
			// BUG #9 FIX: Unclosed string — return an ILLEGAL token with the
			// partial content so the parser can report a clear error.
			return Token{Type: TokenIllegal, Value: "unclosed string: \"" + str}
		}
		return Token{Type: TokenString, Value: str}

	// ── EOF ───────────────────────────────────────────────────────────────
	case 0:
		return Token{Type: TokenEOF, Value: ""}

	// ── Multi-character tokens ────────────────────────────────────────────
	default:
		if isLetter(l.ch) {
			ident := l.readIdentifier()
			// LookupIdentifier promotes reserved words to their keyword type.
			return Token{Type: LookupIdentifier(ident), Value: ident}
		}
		if isDigit(l.ch) {
			num := l.readNumber()
			return Token{Type: TokenNumber, Value: num}
		}
		// Every unrecognised byte becomes an ILLEGAL token so parsing can
		// continue and accumulate all errors rather than aborting on the first.
		tok := Token{Type: TokenIllegal, Value: string(l.ch)}
		l.readChar()
		return tok
	}
}

// ── Character-class predicates ────────────────────────────────────────────────

// isLetter reports whether b is a valid identifier constituent character.
// The underscore is included so that idiomatic_snake_case names are supported.
func isLetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || b == '_'
}

// isDigit reports whether b is an ASCII decimal digit.
func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
