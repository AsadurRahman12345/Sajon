// planner.go — Sajon Dry-Run Blueprint Engine
//
// Implements the `sajon plan` command.  It reads the parsed AST and the
// current sajon.lock state file, then prints a clear human-readable blueprint
// of every action that `sajon up` WOULD take — without touching any cloud API,
// writing any file to disk, or spinning up any container.
//
// Output legend:
//   [~]  Resource already live in sajon.lock   → No changes
//   [+]  Resource not in lock                  → Will be created
//   [-]  Resource in lock but status != active → Will be re-provisioned
//
// The Planner is intentionally read-only: it never calls Flush(), UpsertResource(),
// or any HTTP client.
package emitter

import (
	"fmt"
	"strings"

	"sajon/pkg/parser"
)

// ── PlanAction describes a single planned action ──────────────────────────────

// ActionKind classifies what the planner decided for a resource.
type ActionKind int

const (
	ActionNoChange   ActionKind = iota // resource is live in lock
	ActionCreate                       // resource is absent from lock
	ActionReprovision                  // resource in lock but status != "active"
	ActionOrphan                       // resource in lock but NOT in current AST (rename guard)
)

// PlanAction holds a single planned action with all display metadata.
type PlanAction struct {
	Kind         ActionKind
	ResourceName string
	ResourceKind string // RESOURCE | DATABASE | WORKER | ENDPOINT | ENV | SERVER | STORAGE
	Provider     string // neon | aws | supabase | postgres | bigquery | ""
	CloudType    string // postgres | s3 | ec2 | rds | ""
	Detail       string // extra human-readable detail line
}

// ── Planner struct ────────────────────────────────────────────────────────────

// Planner walks the AST, checks sajon.lock, and produces a sorted list of
// PlanActions without side-effects.
type Planner struct {
	program  *parser.Program
	lockFile *LockFile
	Force    bool // when true, shows orphan warning (non-blocking for plan)
	Actions  []PlanAction
	Log      []string // human-readable plan events for terminal display
}

// NewPlanner constructs a Planner for the given program.
// Call Plan() to populate Actions.
func NewPlanner(p *parser.Program) *Planner {
	return &Planner{program: p}
}

// ── Plan ──────────────────────────────────────────────────────────────────────

// Plan reads sajon.lock and walks the AST, populating pl.Actions.
// It is completely read-only and safe to call multiple times.
// Orphaned resources (in lock but not in AST) are surfaced as [!] ORPHAN
// plan actions — the plan is non-blocking, but 'sajon up' will abort unless
// --force is passed.
func (pl *Planner) Plan() error {
	// Load lock (missing / corrupted → empty lock, no error surfaced to caller)
	lf, err := ReadLockFile()
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	pl.lockFile = lf

	// ── Orphan detection ────────────────────────────────────────────────────
	currentNames := make(map[string]bool)
	for _, stmt := range pl.program.Statements {
		if rs, ok := stmt.(*parser.ResourceStatement); ok {
			currentNames[rs.Name] = true
		}
	}
	for name, lr := range lf.Resources {
		if lr.Status == "active" && !currentNames[name] {
			pl.Actions = append(pl.Actions, PlanAction{
				Kind:         ActionOrphan,
				ResourceName: name,
				ResourceKind: lr.Type,
				Provider:     lr.Provider,
				CloudType:    lr.Provider + " resource",
				Detail: fmt.Sprintf(
					"project_id: %s | ORPHAN: resource is in sajon.lock but NOT in app.saj."+
					" Rename guard will BLOCK 'sajon up' unless --force is passed.",
					lr.ProjectID),
			})
		}
	}

	for _, stmt := range pl.program.Statements {
		switch node := stmt.(type) {
		case *parser.ResourceStatement:
			pl.planResource(node)
		case *parser.EndpointStatement:
			pl.planEndpoint(node)
		case *parser.EnvStatement:
			pl.planEnv(node)
		}
	}
	return nil
}

// ── Per-node planners ─────────────────────────────────────────────────────────

func (pl *Planner) planResource(rs *parser.ResourceStatement) {
	provider := propValue(rs, "provider")
	engine   := propValue(rs, "engine")
	region   := propValue(rs, "region")
	version  := propValue(rs, "version")

	// Determine the canonical cloud type label for display.
	cloudType := resolveCloudType(rs)

	// Build the human-readable detail line.
	var parts []string
	if provider != "" {
		parts = append(parts, "provider: "+provider)
	}
	if engine != "" {
		parts = append(parts, "engine: "+engine)
	}
	if region != "" {
		parts = append(parts, "region: "+region)
	}
	if version != "" {
		parts = append(parts, "version: "+version)
	}
	detail := strings.Join(parts, "  |  ")

	// Check lock state (only Neon/postgres resources are tracked in the lock).
	isLockTracked := isNeonEligible(rs) || provider == "aws" || provider == "supabase"

	if isLockTracked {
		if cached, found := pl.lockFile.GetActiveResource(rs.Name); found {
			pl.Actions = append(pl.Actions, PlanAction{
				Kind:         ActionNoChange,
				ResourceName: rs.Name,
				ResourceKind: rs.Kind,
				Provider:     cached.Provider,
				CloudType:    cloudType,
				Detail:       fmt.Sprintf("project_id: %s", cached.ProjectID),
			})
			return
		}
		// Check for a failed/stale entry.
		if lr, exists := pl.lockFile.Resources[rs.Name]; exists && lr.Status != "active" {
			pl.Actions = append(pl.Actions, PlanAction{
				Kind:         ActionReprovision,
				ResourceName: rs.Name,
				ResourceKind: rs.Kind,
				Provider:     provider,
				CloudType:    cloudType,
				Detail:       fmt.Sprintf("previous status: %s  →  will retry", lr.Status),
			})
			return
		}
	}

	pl.Actions = append(pl.Actions, PlanAction{
		Kind:         ActionCreate,
		ResourceName: rs.Name,
		ResourceKind: rs.Kind,
		Provider:     provider,
		CloudType:    cloudType,
		Detail:       detail,
	})
}

func (pl *Planner) planEndpoint(es *parser.EndpointStatement) {
	pl.Actions = append(pl.Actions, PlanAction{
		Kind:         ActionCreate,
		ResourceName: es.Method + " " + es.Path,
		ResourceKind: "ENDPOINT",
		Detail:       fmt.Sprintf("route: %s %s", es.Method, es.Path),
	})
}

func (pl *Planner) planEnv(es *parser.EnvStatement) {
	pl.Actions = append(pl.Actions, PlanAction{
		Kind:         ActionCreate,
		ResourceName: es.Name,
		ResourceKind: "ENV",
		Detail:       fmt.Sprintf("%d variable(s) → injected into all services", len(es.Vars)),
	})
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// resolveCloudType returns a descriptive label for what type of resource this is.
func resolveCloudType(rs *parser.ResourceStatement) string {
	provider := propValue(rs, "provider")
	engine   := propValue(rs, "engine")
	rsType   := propValue(rs, "type")

	switch {
	case provider == "neon" && rsType == "postgres":
		return "Neon Postgres Database"
	case provider == "postgres":
		return "Neon Postgres Database"
	case engine == "postgres":
		return "Neon Postgres Database"
	case provider == "aws" && rs.Kind == "STORAGE":
		return "AWS S3 Storage Bucket"
	case provider == "aws" && rs.Kind == "SERVER":
		return "AWS EC2 Instance"
	case provider == "aws" && engine == "postgres":
		return "AWS RDS Postgres Instance"
	case provider == "aws":
		return "AWS Resource"
	case provider == "supabase":
		return "Supabase Project"
	case engine == "bigquery":
		return "BigQuery Dataset (cloud-managed)"
	case rs.Kind == "WORKER":
		return "Background Worker"
	default:
		return rs.Kind
	}
}

// isNeonEligible mirrors the same check in CloudEmitter.ProvisionAll.
func isNeonEligible(rs *parser.ResourceStatement) bool {
	return propValue(rs, "provider") == "postgres" ||
		propValue(rs, "engine") == "postgres" ||
		(propValue(rs, "provider") == "neon" && propValue(rs, "type") == "postgres")
}
