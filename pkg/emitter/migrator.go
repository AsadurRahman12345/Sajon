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

// collectSchemas is a convenience helper that extracts all non-nil SchemaBlocks
// from a single ResourceStatement.  The result is a slice (currently length 0
// or 1) but the slice form keeps the RunMigrations signature future-proof for
// multi-table schemas.
func collectSchemas(rs *parser.ResourceStatement) []*parser.SchemaBlock {
	if rs.Schema != nil {
		return []*parser.SchemaBlock{rs.Schema}
	}
	return nil
}
