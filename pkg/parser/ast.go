// Package parser defines the Abstract Syntax Tree (AST) node types for the
// Sajon (.saj) language.  Every syntactic construct the parser recognises maps
// to one concrete type in this file, keeping AST definitions strictly separate
// from the parsing logic that produces them.
package parser

import (
	"bytes"
	"fmt"
	"strings"
)

// ── Base interfaces ───────────────────────────────────────────────────────────

// Node is the root interface that every AST node must satisfy.
// TokenLiteral is used in tests and error messages; String provides a
// canonical, human-readable rendering of the subtree.
type Node interface {
	TokenLiteral() string
	String() string
}

// Statement is a Node that forms a complete top-level or block-level construct.
// The marker method statementNode() makes the type system enforce that only
// genuine statement types can appear where statements are expected.
type Statement interface {
	Node
	statementNode()
}

// ── Shared sub-structures ─────────────────────────────────────────────────────

// Property represents a single key : value pair inside a block body.
// Both Key and Value are stored as plain strings; the parser resolves
// any numeric literal to its string representation for simplicity at this stage.
type Property struct {
	Key   string // raw identifier key, e.g. "provider"
	Value string // resolved literal value, e.g. "postgres" or "15"
}

// String formats the property as "  key : value" for AST dumps.
func (p Property) String() string {
	return fmt.Sprintf("    %-14s : %s", p.Key, p.Value)
}

// ── Program ───────────────────────────────────────────────────────────────────

// Program is the root node of every Sajon AST.  The parser populates its
// Statements slice in source order; downstream passes iterate this slice.
type Program struct {
	Statements []Statement
}

// TokenLiteral delegates to the first statement, or returns "" for empty input.
func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

// String renders the entire program tree as an indented, human-readable dump.
func (p *Program) String() string {
	var out bytes.Buffer
	for _, s := range p.Statements {
		out.WriteString(s.String())
		out.WriteString("\n")
	}
	return out.String()
}

// ── ResourceStatement ─────────────────────────────────────────────────────────

// ResourceStatement represents any property-block declaration whose keyword is
// one of: RESOURCE, DATABASE, WORKER.  The Kind field preserves which keyword
// introduced the block so that later passes can dispatch on block semantics.
//
// Syntax:  <Kind> <Name> { <key>: <value> ... }
//
// Example:
//
//	RESOURCE user_db {
//	    provider: "postgres"
//	    version:  15
//	}
type ResourceStatement struct {
	Kind       string        // "RESOURCE" | "DATABASE" | "WORKER"
	Name       string        // user-defined block identifier
	Properties []Property    // ordered list of key-value pairs
	Schemas    []*SchemaBlock // all inline SCHEMA blocks (multi-table support)
	Datas      []*DataBlock  // all inline DATA blocks (multi-table support)
}

func (rs *ResourceStatement) statementNode() {}

// TokenLiteral returns the keyword that opened this block.
func (rs *ResourceStatement) TokenLiteral() string { return rs.Kind }

// String renders the block as a compact indented representation.
func (rs *ResourceStatement) String() string {
	var out bytes.Buffer
	out.WriteString(fmt.Sprintf("[%s] %s\n", rs.Kind, rs.Name))
	for _, p := range rs.Properties {
		out.WriteString(p.String() + "\n")
	}
	for _, schema := range rs.Schemas {
		out.WriteString(fmt.Sprintf("    SCHEMA → table: %s  fields: %s\n",
			schema.Table, strings.Join(schema.Fields, ", ")))
	}
	for _, data := range rs.Datas {
		out.WriteString(fmt.Sprintf("    DATA → insert_into: %s  rows: %d\n",
			data.InsertInto, len(data.Rows)))
	}
	return out.String()
}

// ── SchemaBlock ───────────────────────────────────────────────────────────────

// SchemaBlock describes the desired database table schema that the compiler
// will automatically create (via CREATE TABLE IF NOT EXISTS) on the live
// database after provisioning.
//
// Syntax (inside a RESOURCE / DATABASE block):
//
//	SCHEMA {
//	    table:  "users"
//	    fields: ["id:int", "name:string", "email:string"]
//	}
//
// Field format: "<column_name>:<type>"  where type is one of:
//
//	int | string | text | bool | float | timestamp | uuid
type SchemaBlock struct {
	Table  string   // destination table name, e.g. "users"
	Fields []string // field descriptors, e.g. "id:int", "name:string"
}

// ── DataBlock ─────────────────────────────────────────────────────────────────

// DataRow is an ordered list of column-name/value pairs for a single seed row.
// Using a slice (not a map) preserves the declaration order in the .saj file,
// which keeps generated INSERT column lists stable across compiler runs.
type DataRow struct {
	Columns []Property // ordered column-name : value pairs for this row
}

// DataBlock describes one or more seed rows to INSERT into a live table after
// the schema migration has been applied.  Each INSERT uses ON CONFLICT DO
// NOTHING so repeated 'sajon up' runs never produce duplicate rows.
//
// Syntax (inside a RESOURCE / DATABASE block):
//
//	DATA {
//	    insert_into: "users"
//	    row: { id: 1  name: "Alice"  email: "alice@example.com" }
//	    row: { id: 2  name: "Bob"    email: "bob@example.com"   }
//	}
type DataBlock struct {
	InsertInto string    // target table name, e.g. "users"
	Rows       []DataRow // seed rows in declaration order
}

// ── EndpointStatement ─────────────────────────────────────────────────────────

// EndpointStatement represents an HTTP handler declaration.
//
// Syntax:  ENDPOINT <Method> <Path> { <body statements> }
//
// Example:
//
//	ENDPOINT POST "/signup" {
//	    RETURN "Success"
//	}
type EndpointStatement struct {
	Method string      // HTTP verb identifier, e.g. "POST", "GET"
	Path   string      // Route string literal, e.g. "/signup"
	Body   []Statement // Ordered list of statements inside the block
}

func (es *EndpointStatement) statementNode() {}

// TokenLiteral returns the "ENDPOINT" keyword literal.
func (es *EndpointStatement) TokenLiteral() string { return "ENDPOINT" }

// String renders the endpoint block with its body statements indented.
func (es *EndpointStatement) String() string {
	var out bytes.Buffer
	out.WriteString(fmt.Sprintf("[ENDPOINT] %s \"%s\"\n", es.Method, es.Path))
	for _, stmt := range es.Body {
		// indent body statements by 4 spaces
		for _, line := range strings.Split(stmt.String(), "\n") {
			if line != "" {
				out.WriteString("    " + line + "\n")
			}
		}
	}
	return out.String()
}

// ── ReturnStatement ───────────────────────────────────────────────────────────

// ReturnStatement represents a RETURN expression inside an endpoint or worker body.
//
// Syntax:  RETURN <value>
//
// Example:  RETURN "Success"
type ReturnStatement struct {
	Value string // the literal value (string or identifier) to return
}

func (rs *ReturnStatement) statementNode() {}

// TokenLiteral returns the "RETURN" keyword literal.
func (rs *ReturnStatement) TokenLiteral() string { return "RETURN" }

// String renders the statement as "RETURN <value>".
func (rs *ReturnStatement) String() string {
	return fmt.Sprintf("RETURN \"%s\"", rs.Value)
}

// ── EnvStatement ──────────────────────────────────────────────────────────────

// EnvStatement represents a named block of environment variable declarations.
// At emit time every key-value pair is injected into the environment: section
// of ALL generated Docker services, making secrets and config universally
// available without manual wiring.
//
// Syntax:  ENV <Name> { <KEY>: <"value"> ... }
//
// Example:
//
//	ENV production {
//	    SECRET_KEY: "saju_super_secret"
//	    DEBUG_MODE: "false"
//	}
type EnvStatement struct {
	Name string     // block label, e.g. "production"
	Vars []Property // ordered list of KEY: "value" pairs
}

func (es *EnvStatement) statementNode() {}

// TokenLiteral returns the "ENV" keyword literal.
func (es *EnvStatement) TokenLiteral() string { return "ENV" }

// String renders the ENV block as an indented key-value listing.
func (es *EnvStatement) String() string {
	var out bytes.Buffer
	out.WriteString(fmt.Sprintf("[ENV] %s\n", es.Name))
	for _, v := range es.Vars {
		out.WriteString(v.String() + "\n")
	}
	return out.String()
}
