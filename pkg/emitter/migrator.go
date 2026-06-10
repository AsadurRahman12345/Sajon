// migrator.go — Sajon Declarative Auto-Migration: Live Database Executor
//
// RunMigrations connects to a freshly-provisioned Postgres database using the
// standard database/sql interface and executes the CREATE TABLE statements
// compiled by schema_compiler.go.
//
// It is intentionally called ONLY in live mode (real credentials present) so
// that simulation runs never attempt to dial a non-existent host.
//
// Retry policy: Postgres on Supabase / Neon may need a few seconds after
// reporting ACTIVE_HEALTHY before the connection layer is fully ready.
// RunMigrations retries the initial Ping up to 3 times with a 5-second
// back-off before giving up, which covers 99 % of cold-start races.

package emitter

import (
	"database/sql"
	"fmt"
	"time"

	// Register the lib/pq PostgreSQL driver with database/sql.
	// The blank import is the conventional registration mechanism.
	_ "github.com/lib/pq"

	"sajon/pkg/parser"
)

// migrateRetries controls how many times RunMigrations retries a failed Ping
// before declaring the database unreachable.
const migrateRetries = 3

// migratePingDelay is the wait between successive Ping attempts.
const migratePingDelay = 5 * time.Second

// RunMigrations connects to the Postgres database at connStr and executes a
// CREATE TABLE IF NOT EXISTS statement for each provided SchemaBlock.
//
// Parameters:
//
//	connStr      — PostgreSQL DSN / connection URL produced by the provisioner.
//	schemas      — Slice of SchemaBlocks gathered from the parsed program's AST.
//	resourceName — Human-readable label for log messages (the resource name).
//
// Returns a non-nil error only when the database is unreachable or a SQL
// statement fails.  Individual "table already exists" scenarios are swallowed
// by the IF NOT EXISTS clause, making migrations fully idempotent.
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

	// ── Ping with retry for cold-start databases ───────────────────────────
	var pingErr error
	for attempt := 1; attempt <= migrateRetries; attempt++ {
		if pingErr = db.Ping(); pingErr == nil {
			break
		}
		if attempt < migrateRetries {
			fmt.Printf("     [⚡] Auto-Migration: DB not ready yet (attempt %d/%d) — retrying in %s...\n",
				attempt, migrateRetries, migratePingDelay)
			time.Sleep(migratePingDelay)
		}
	}
	if pingErr != nil {
		return fmt.Errorf("auto-migration ping failed after %d attempts: %w", migrateRetries, pingErr)
	}

	// ── Execute each schema ────────────────────────────────────────────────
	for _, schema := range schemas {
		if schema == nil {
			continue
		}
		stmt := CompileSchema(schema)
		if stmt == "" {
			continue // nothing to run (empty schema)
		}

		fmt.Printf("     [⚡] Auto-Migration: Executing — %s\n", stmt)

		if _, execErr := db.Exec(stmt); execErr != nil {
			return fmt.Errorf(
				"auto-migration exec for table '%s' on resource '%s': %w",
				schema.Table, resourceName, execErr,
			)
		}

		fmt.Printf("     [⚡] Auto-Migration Success: Table '%s' generated dynamically inside the cloud database!\n",
			schema.Table)
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
