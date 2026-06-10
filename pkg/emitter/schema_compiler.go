// schema_compiler.go — Sajon Declarative Auto-Migration: SQL Translation Engine
//
// Translates a SchemaBlock AST node (produced by the parser from a .saj SCHEMA
// block) into a valid PostgreSQL CREATE TABLE IF NOT EXISTS statement.
//
// Type mapping table:
//
//	int (field "id")  →  SERIAL PRIMARY KEY
//	int (other)       →  INTEGER
//	string            →  VARCHAR(255)
//	text              →  TEXT
//	bool              →  BOOLEAN DEFAULT TRUE
//	float             →  NUMERIC(10,2)
//	timestamp         →  TIMESTAMP DEFAULT NOW()
//	uuid              →  UUID DEFAULT gen_random_uuid()
//	<anything else>   →  VARCHAR(255)   (safe fallback)

package emitter

import (
	"fmt"
	"strings"

	"sajon/pkg/parser"
)

// CompileSchema translates a *parser.SchemaBlock into a ready-to-execute
// PostgreSQL CREATE TABLE statement.
//
// Returns an empty string when the schema is nil, has no table name, or has
// no fields — callers should treat an empty return value as "nothing to do".
func CompileSchema(schema *parser.SchemaBlock) string {
	if schema == nil || schema.Table == "" || len(schema.Fields) == 0 {
		return ""
	}

	cols := make([]string, 0, len(schema.Fields))
	for _, field := range schema.Fields {
		parts := strings.SplitN(field, ":", 2)
		if len(parts) != 2 {
			continue // skip malformed descriptors gracefully
		}
		colName := strings.TrimSpace(parts[0])
		colType := strings.TrimSpace(strings.ToLower(parts[1]))
		if colName == "" {
			continue
		}
		cols = append(cols, mapFieldToSQL(colName, colType))
	}

	if len(cols) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"CREATE TABLE IF NOT EXISTS %s (%s);",
		schema.Table,
		strings.Join(cols, ", "),
	)
}

// mapFieldToSQL converts a single field name + type pair into a PostgreSQL
// column definition string.
//
// Special-case: a field literally named "id" with type "int" is promoted to
// SERIAL PRIMARY KEY (auto-increment primary key) — the most common pattern.
func mapFieldToSQL(name, typ string) string {
	switch typ {
	case "int", "integer":
		if name == "id" {
			return "id SERIAL PRIMARY KEY"
		}
		return fmt.Sprintf("%s INTEGER", name)

	case "string", "varchar":
		return fmt.Sprintf("%s VARCHAR(255)", name)

	case "text":
		return fmt.Sprintf("%s TEXT", name)

	case "bool", "boolean":
		return fmt.Sprintf("%s BOOLEAN DEFAULT TRUE", name)

	case "float", "double", "numeric":
		return fmt.Sprintf("%s NUMERIC(10,2)", name)

	case "timestamp", "datetime":
		return fmt.Sprintf("%s TIMESTAMP DEFAULT NOW()", name)

	case "uuid":
		return fmt.Sprintf("%s UUID DEFAULT gen_random_uuid()", name)

	case "json", "jsonb":
		return fmt.Sprintf("%s JSONB", name)

	default:
		// Unknown type — default to VARCHAR(255) so the migration never
		// panics; the user can always alter the column type afterwards.
		return fmt.Sprintf("%s VARCHAR(255) /* unknown type: %s */", name, typ)
	}
}
