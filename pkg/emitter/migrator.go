// migrator.go — Sajon Declarative Auto-Migration: Live Database Executor
//
// RunMigrations connects to a freshly-provisioned Postgres database using the
// standard database/sql interface and executes the CREATE TABLE statements
// compiled by schema_compiler.go.
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

// RunMigrations connects to the Postgres database at connStr and executes a
// CREATE TABLE IF NOT EXISTS statement for each provided SchemaBlock.
//
// Parameters:
//
//	connStr      — PostgreSQL DSN / connection URL produced by the provisioner.
//	schemas      — Slice of SchemaBlocks gathered from the parsed program's AST.
//	resourceName — Human-readable label for log messages (the resource name).
//
// Returns a non-nil error only when the database is unreachable after all
// retry attempts, or when a SQL statement fails.  Individual "table already
// exists" scenarios are swallowed by the IF NOT EXISTS clause, making
// migrations fully idempotent.
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
