// migrator.go — Sajon Declarative Auto-Migration: Live Database Executor
//
// RunMigrations connects to a freshly-provisioned Postgres database using the
// standard database/sql interface and executes the correct migration for each
// SchemaBlock — CREATE TABLE for new tables, ALTER TABLE ADD COLUMN for new
// fields on existing tables (Schema Reconciliation).
//
// It is intentionally called ONLY in live mode (real credentials present) so
// that simulation runs never attempt to dial a non-existent host.
//
// Retry policy: Supabase / Neon report ACTIVE_HEALTHY before DNS for the
// database host (db.<ref>.supabase.co) has fully propagated, which can take
// 30–60 seconds.  RunMigrations retries the initial Ping up to 20 times with
// a fixed 5-second back-off (up to ~100 s total), which reliably covers
// real-world DNS propagation windows without giving up too early.

package emitter

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	// Register the lib/pq PostgreSQL driver with database/sql.
	// The blank import is the conventional registration mechanism.
	_ "github.com/lib/pq"

	"sajon/pkg/parser"
)

// migrateRetries controls how many times RunMigrations retries a failed Ping
// before declaring the database unreachable.
//
// 20 attempts × 5 s = up to 100 s total wait.  This covers the 30–60 s DNS
// propagation delay that Supabase exhibits after a project becomes
// ACTIVE_HEALTHY, while still failing fast for genuinely broken connections.
const migrateRetries = 20

// migratePingDelay is the fixed wait between successive Ping attempts.
const migratePingDelay = 5 * time.Second

// RunMigrations connects to the Postgres database at connStr and runs a
// schema-aware migration for each provided SchemaBlock:
//
//   - If the table does not exist → CREATE TABLE IF NOT EXISTS (full schema).
//   - If the table already exists → diff live columns against AST fields and
//     emit ALTER TABLE ... ADD COLUMN IF NOT EXISTS for every new field.
//
// Parameters:
//
//	connStr      — PostgreSQL DSN / connection URL produced by the provisioner.
//	schemas      — Slice of SchemaBlocks gathered from the parsed program's AST.
//	resourceName — Human-readable label for log messages (the resource name).
//
// Returns a non-nil error only when the database is unreachable after all
// retry attempts, or when a SQL statement fails.
func RunMigrations(connStr string, schemas []*parser.SchemaBlock, resourceName string) error {
	if connStr == "" || len(schemas) == 0 {
		return nil
	}

	// ── Open connection (lazy — Ping is what actually dials the network) ───
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("auto-migration open connection: %w", err)
	}
	defer db.Close()

	// ── Ping with retry — waits for DNS propagation after ACTIVE_HEALTHY ──
	// Supabase marks a project ACTIVE_HEALTHY before its db.<ref>.supabase.co
	// DNS record resolves globally.  We retry until the host is reachable or
	// we exhaust all attempts, printing a clear status line each time so the
	// user knows the compiler is making progress and not hanging.
	fmt.Printf("     [⚡] Auto-Migration: Waiting for Database DNS to propagate...\n")
	var pingErr error
	for attempt := 1; attempt <= migrateRetries; attempt++ {
		pingErr = db.Ping()
		if pingErr == nil {
			fmt.Printf("     [⚡] Auto-Migration: Database is reachable! (Attempt %d/%d) ✓\n",
				attempt, migrateRetries)
			break
		}
		fmt.Printf("     [⚡] Auto-Migration: Waiting for Database DNS to propagate (Attempt %d/%d)... retrying in %s\n",
			attempt, migrateRetries, migratePingDelay)
		time.Sleep(migratePingDelay)
	}
	if pingErr != nil {
		return fmt.Errorf("auto-migration ping failed after %d attempts: %w", migrateRetries, pingErr)
	}

	// ── Schema-aware migration for each SchemaBlock ────────────────────────
	for _, schema := range schemas {
		if schema == nil {
			continue
		}
		if err := ReconcileSchema(db, schema, resourceName); err != nil {
			return err
		}
	}

	return nil
}

// ReconcileSchema performs a smart, diff-aware migration for a single table:
//
//  1. Queries information_schema.columns to discover which columns the live
//     table already has (returns empty set when the table does not exist yet).
//  2. If the table is brand-new → runs CREATE TABLE IF NOT EXISTS (full schema).
//  3. If the table already exists → compares live columns against the AST fields
//     and runs ALTER TABLE ... ADD COLUMN IF NOT EXISTS for every new column.
//
// This function is idempotent: re-running it on an already-migrated database
// is a safe no-op.
func ReconcileSchema(db *sql.DB, schema *parser.SchemaBlock, resourceName string) error {
	if schema == nil || schema.Table == "" || len(schema.Fields) == 0 {
		return nil
	}

	// ── Step 1: Inspect live columns via information_schema ───────────────
	// We query the public schema only (Supabase/Neon default).
	// The result is an empty set when the table does not exist yet.
	const inspectSQL = `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name   = $1
		ORDER BY ordinal_position`

	rows, err := db.Query(inspectSQL, schema.Table)
	if err != nil {
		return fmt.Errorf("schema inspect for table '%s': %w", schema.Table, err)
	}
	defer rows.Close()

	liveColumns := make(map[string]bool)
	for rows.Next() {
		var colName string
		if scanErr := rows.Scan(&colName); scanErr != nil {
			return fmt.Errorf("schema inspect scan: %w", scanErr)
		}
		liveColumns[strings.ToLower(colName)] = true
	}
	if closeErr := rows.Close(); closeErr != nil {
		return fmt.Errorf("schema inspect close: %w", closeErr)
	}

	tableExists := len(liveColumns) > 0

	// ── Step 2a: Table is new → CREATE TABLE ─────────────────────────────
	if !tableExists {
		createSQL := CompileSchema(schema)
		if createSQL == "" {
			return nil
		}
		fmt.Printf("     [⚡] Schema Reconciliation: Table '%s' not found — creating...\n", schema.Table)
		fmt.Printf("     [⚡] Auto-Migration: Executing — %s\n", createSQL)
		if _, execErr := db.Exec(createSQL); execErr != nil {
			return fmt.Errorf("auto-migration CREATE TABLE '%s' on resource '%s': %w",
				schema.Table, resourceName, execErr)
		}
		fmt.Printf("     [⚡] Auto-Migration Success: Table '%s' created inside the cloud database!\n", schema.Table)
		return nil
	}

	// ── Step 2b: Table exists → diff and ALTER for new columns ───────────
	fmt.Printf("     [⚡] Schema Reconciliation: Table '%s' already exists — checking for new columns...\n", schema.Table)

	newCols := 0
	for _, field := range schema.Fields {
		parts := strings.SplitN(field, ":", 2)
		if len(parts) != 2 {
			continue
		}
		colName := strings.TrimSpace(strings.ToLower(parts[0]))
		colType := strings.TrimSpace(strings.ToLower(parts[1]))
		if colName == "" {
			continue
		}

		// Skip columns that already exist in the live table.
		if liveColumns[colName] {
			fmt.Printf("     [⚡] Schema Reconciliation: Column '%s' already exists — skipping.\n", colName)
			continue
		}

		// Generate the PostgreSQL column definition (reuse type-mapping logic).
		colDef := mapFieldToSQL(colName, colType)

		// ALTER TABLE ... ADD COLUMN IF NOT EXISTS is idempotent and safe.
		alterSQL := fmt.Sprintf(
			"ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s;",
			schema.Table, colDef,
		)

		fmt.Printf("     [⚡] Schema Diff Detected: New column '%s' — applying migration...\n", colName)
		fmt.Printf("     [⚡] Auto-Migration: Executing — %s\n", alterSQL)

		if _, execErr := db.Exec(alterSQL); execErr != nil {
			return fmt.Errorf("auto-migration ALTER TABLE '%s' ADD COLUMN '%s' on resource '%s': %w",
				schema.Table, colName, resourceName, execErr)
		}

		fmt.Printf("     [⚡] Auto-Migration Success: Column '%s' added to table '%s'!\n", colName, schema.Table)
		newCols++
	}

	if newCols == 0 {
		fmt.Printf("     [⚡] Schema Reconciliation: Table '%s' is already up-to-date — no changes needed.\n", schema.Table)
	} else {
		fmt.Printf("     [⚡] Schema Reconciliation: %d new column(s) added to table '%s'.\n", newCols, schema.Table)
	}

	return nil
}

// collectSchemas returns all SchemaBlocks declared inside a ResourceStatement.
// With multi-table support, a single RESOURCE can have N SCHEMA blocks and
// this function returns all of them so RunMigrations processes every table.
func collectSchemas(rs *parser.ResourceStatement) []*parser.SchemaBlock {
	return rs.Schemas
}


// RunSeed opens a fresh database connection to connStr and seeds data from the
// provided DataBlock.  It is called after RunMigrations so the target table is
// guaranteed to exist before any INSERT is attempted.
func RunSeed(connStr string, data *parser.DataBlock, resourceName string) error {
	if connStr == "" || data == nil || len(data.Rows) == 0 {
		return nil
	}

	db, err := sql.Open("postgres", connStr)
	if err != nil {
		return fmt.Errorf("seed open connection: %w", err)
	}
	defer db.Close()

	return SeedData(db, data, resourceName)
}



// SeedData executes the INSERT statements described by a DataBlock on the live
// database.  It is called after schema migration so the target table is
// guaranteed to exist.
//
// Each row becomes:
//
//	INSERT INTO <table> (<col1>, <col2>, ...) VALUES (<v1>, <v2>, ...)
//	ON CONFLICT DO NOTHING;
//
// ON CONFLICT DO NOTHING makes seeding fully idempotent — repeated 'sajon up'
// runs never produce duplicate rows even if the user re-runs after a partial
// success.
//
// String values are single-quote escaped; numeric values are inserted unquoted.
func SeedData(db *sql.DB, data *parser.DataBlock, resourceName string) error {
	if data == nil || data.InsertInto == "" || len(data.Rows) == 0 {
		return nil
	}

	fmt.Printf("     [⚡] Seeding data into table '%s'...\n", data.InsertInto)

	for i, row := range data.Rows {
		if len(row.Columns) == 0 {
			continue
		}

		// Build column list and value list in declaration order.
		cols := make([]string, 0, len(row.Columns))
		vals := make([]string, 0, len(row.Columns))
		for _, col := range row.Columns {
			cols = append(cols, col.Key)
			vals = append(vals, formatSQLValue(col.Value))
		}

		insertSQL := fmt.Sprintf(
			"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING;",
			data.InsertInto,
			strings.Join(cols, ", "),
			strings.Join(vals, ", "),
		)

		fmt.Printf("     [⚡] Seeding row %d — %s\n", i+1, insertSQL)

		if _, execErr := db.Exec(insertSQL); execErr != nil {
			return fmt.Errorf(
				"seed data INSERT into '%s' row %d on resource '%s': %w",
				data.InsertInto, i+1, resourceName, execErr,
			)
		}

		fmt.Printf("     [⚡] Seeded row %d into '%s' ✓\n", i+1, data.InsertInto)
	}

	fmt.Printf("     [⚡] Data Seeding Complete: %d row(s) inserted into '%s'.\n",
		len(data.Rows), data.InsertInto)
	return nil
}

// formatSQLValue converts a string value from the AST into a safe SQL literal.
// Numeric values (integer or decimal) are emitted unquoted; everything else is
// wrapped in single quotes with internal single quotes doubled for safety.
func formatSQLValue(v string) string {
	if isNumeric(v) {
		return v
	}
	// Escape single quotes by doubling them (standard SQL escaping).
	escaped := strings.ReplaceAll(v, "'", "''")
	return "'" + escaped + "'"
}

// isNumeric reports whether s is a valid integer or decimal literal.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i, c := range s {
		if c == '.' {
			if dot {
				return false // two dots
			}
			dot = true
			continue
		}
		if c == '-' && i == 0 {
			continue // leading minus
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

