// cloud_emitter.go — Sajon Cloud Provisioner
//
// Implements real cloud database provisioning via the Neon HTTP API.
// When NEON_API_KEY is set, each postgres ResourceStatement in the AST
// results in a live, ready-to-use Neon Serverless Postgres project being
// created and its connection string printed to the terminal.
//
// STATE MANAGEMENT (sajon.lock)
// ─────────────────────────────
// Before hitting the Neon API the emitter reads sajon.lock from the current
// working directory.  If the lock contains an "active" entry for the resource
// the API call is skipped entirely and the cached metadata is reused.
// On a fresh (or first-time) provision the lock is written / updated so that
// subsequent runs are idempotent.
package emitter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"sajon/pkg/parser"
)

// ── API constants ─────────────────────────────────────────────────────────────

const (
	neonAPIBase    = "https://console.neon.tech/api/v2"
	neonAPITimeout = 30 * time.Second
)

// ── Public result type ────────────────────────────────────────────────────────

// CloudResult holds the provisioned database details returned by Neon after a
// successful project creation.  All fields are populated from either the live
// API response or the cached sajon.lock and are safe to embed in a
// docker-compose.yml or .env file.
type CloudResult struct {
	ResourceName     string // original Sajon resource name  (e.g. "user_db")
	ProjectID        string // Neon-assigned project ID       (e.g. "proj_abc123")
	ProjectName      string // project name sent to Neon      (e.g. "sajon-user-db")
	Region           string // Neon region                    (e.g. "aws-ap-south-1")
	Host             string // serverless host                (e.g. "ep-xxx.aws.neon.tech")
	PoolerHost       string // connection-pooler host
	Database         string // default database name          (e.g. "neondb")
	User             string // database role / user
	Password         string // generated password (sensitive — never log)
	ConnectionString string // full postgres:// URI with sslmode=require
	FromCache        bool   // true when result was loaded from sajon.lock
}

// ── CloudEmitter struct ───────────────────────────────────────────────────────

// CloudEmitter traverses the Sajon AST and provisions real cloud databases
// via the Neon Serverless Postgres API.  One CloudResult is produced per
// matched ResourceStatement.
type CloudEmitter struct {
	program  *parser.Program
	apiKey   string   // Bearer token from NEON_API_KEY
	orgID    string   // Neon organization ID (overrides built-in default)
	lockFile *LockFile // in-memory state loaded from sajon.lock
	Force    bool     // when true, orphan-guard warnings do not block execution
	Results  []CloudResult
	Log      []string // human-readable provisioning events for terminal display
}

// NewCloud creates a CloudEmitter.
// apiKey must be a valid Neon Personal API key (read from NEON_API_KEY).
// orgID may be empty — the emitter falls back to the account's default org.
func NewCloud(p *parser.Program, apiKey, orgID string) *CloudEmitter {
	return &CloudEmitter{
		program: p,
		apiKey:  apiKey,
		orgID:   orgID,
	}
}

// ── Core provisioning ─────────────────────────────────────────────────────────

// ProvisionAll walks the entire AST and calls the Neon API for every
// ResourceStatement that represents a Neon-eligible postgres database.
//
// A ResourceStatement is Neon-eligible when ANY of the following hold:
//
//	(a) provider == "postgres"                          (legacy form)
//	(b) engine   == "postgres"                          (DATABASE block form)
//	(c) provider == "neon"  AND  type == "postgres"     (new RESOURCE … DATABASE form)
//
// Resources whose provider is "aws" or "supabase" are intentionally skipped
// here — they are routed to AWSEmitter / SupabaseEmitter by main.go.
//
// STATE MANAGEMENT: Before calling the Neon API for any resource the method
// checks sajon.lock.  Active (already-provisioned) resources are loaded from
// the lock and returned without touching the API.
//
// ORPHAN GUARD: If the lock contains resources that are no longer present in
// the AST, execution is aborted unless ce.Force == true.  This prevents
// silent data loss when a resource is renamed in app.saj.
func (ce *CloudEmitter) ProvisionAll() error {
	// ── Step 0: Load the lock file ─────────────────────────────────────────
	lf, err := ReadLockFile()
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}
	ce.lockFile = lf

	// ── Step 0b: Orphan guard ──────────────────────────────────────────────
	// Build the set of resource names currently in the AST.
	currentNames := make(map[string]bool)
	for _, stmt := range ce.program.Statements {
		if rs, ok := stmt.(*parser.ResourceStatement); ok {
			currentNames[rs.Name] = true
		}
	}
	// Find lock entries whose names are no longer in the AST.
	var orphans []string
	for name, lr := range lf.Resources {
		if lr.Status == "active" && !currentNames[name] {
			orphanMsg := fmt.Sprintf("  ⚠️  ORPHAN: '%s' (provider: %s  project_id: %s) is in sajon.lock\n"+
				"       but NOT in the current app.saj.\n"+
				"       If this resource was renamed, the old cloud resource will NOT be deleted\n"+
				"       automatically. Run 'sajon down' first, or pass --force to override.",
				name, lr.Provider, lr.ProjectID)
			orphans = append(orphans, orphanMsg)
		}
	}
	if len(orphans) > 0 {
		for _, o := range orphans {
			ce.addLog(o)
			fmt.Println(o)
		}
		if !ce.Force {
			return fmt.Errorf("%d orphaned remote resource(s) detected — "+
				"run 'sajon up --force' to bypass this guard or rename resources back", len(orphans))
		}
		ce.addLog(fmt.Sprintf("[⚠️ FORCED] Proceeding despite %d orphaned resource(s).", len(orphans)))
		fmt.Printf("  [⚠️ FORCED] Proceeding despite %d orphaned resource(s).\n", len(orphans))
	}

	// ── Step 1: Count eligible resources ───────────────────────────────────
	eligibleCount := 0
	for _, stmt := range ce.program.Statements {
		rs, ok := stmt.(*parser.ResourceStatement)
		if !ok {
			continue
		}
		if propValue(rs, "provider") == "aws" || propValue(rs, "provider") == "supabase" {
			continue
		}
		isPostgres := propValue(rs, "provider") == "postgres" ||
			propValue(rs, "engine") == "postgres" ||
			(propValue(rs, "provider") == "neon" && propValue(rs, "type") == "postgres")
		if isPostgres {
			eligibleCount++
		}
	}

	if eligibleCount == 0 {
		ce.addLog("No Neon-eligible postgres resources found — nothing provisioned.")
		return nil
	}

	// ── Step 2: Resolve org ID (only needed for live API calls) ────────────
	// We defer this until we know at least one resource will actually need
	// a live API call — lock-file hits skip the org resolution entirely.
	orgResolved := false

	// ── Step 3: Provision / restore each eligible resource ─────────────────
	provisioned := 0
	for _, stmt := range ce.program.Statements {
		rs, ok := stmt.(*parser.ResourceStatement)
		if !ok {
			continue
		}
		// Skip AWS resources — handled by AWSEmitter.
		if propValue(rs, "provider") == "aws" {
			continue
		}
		// Skip Supabase resources — handled by SupabaseEmitter.
		if propValue(rs, "provider") == "supabase" {
			continue
		}

		// Determine whether this is a Neon-eligible postgres resource.
		//
		// (a) RESOURCE <name> { provider: "postgres" ... }
		// (b) DATABASE <name> { engine: "postgres" ... }
		// (c) RESOURCE <name> DATABASE { provider: "neon" type: "postgres" }
		//     (parsed as Kind="DATABASE", provider="neon", type="postgres")
		isPostgres := propValue(rs, "provider") == "postgres" ||
			propValue(rs, "engine") == "postgres" ||
			(propValue(rs, "provider") == "neon" && propValue(rs, "type") == "postgres")
		if !isPostgres {
			continue
		}

		// ── Lock-file pre-check ───────────────────────────────────────────────
		if cached, found := ce.lockFile.GetActiveResource(rs.Name); found {
			// Resource is already live — skip the API call and reuse metadata.
			ce.addLog(fmt.Sprintf(
				"[ℹ️ ] Resource '%s' is already live. Skipping cloud provisioning.", rs.Name,
			))
			result := CloudResult{
				ResourceName:     rs.Name,
				ProjectID:        cached.ProjectID,
				ProjectName:      "sajon-" + strings.ReplaceAll(rs.Name, "_", "-"),
				Region:           cached.Region, // FIX: restore persisted region from lock
				Host:             cached.Host,
				PoolerHost:       cached.PoolerHost,
				Database:         cached.Database,
				User:             cached.User,
				ConnectionString: cached.ConnectionString,
				FromCache:        true,
			}
			ce.Results = append(ce.Results, result)
			provisioned++

			// ── Schema Reconciliation on cached resource ──────────────────────
			// The project already exists in sajon.lock but the user may have
			// added new fields to the SCHEMA block.  RunMigrations diffs
			// information_schema.columns and emits ALTER TABLE ADD COLUMN
			// IF NOT EXISTS for any new fields found in the AST.
			if schemas := collectSchemas(rs); len(schemas) > 0 && ce.apiKey != "" && cached.ConnectionString != "" {
				ce.addLog(fmt.Sprintf("[⚡] Schema Reconciliation: checking for schema changes on cached resource '%s'...", rs.Name))
				fmt.Printf("     [⚡] Schema Reconciliation: '%s' is cached — checking for new columns in SCHEMA block...\n", rs.Name)
				if migErr := RunMigrations(cached.ConnectionString, schemas, rs.Name); migErr != nil {
					ce.addLog(fmt.Sprintf("[⚠️ ] Schema reconciliation warning for '%s': %v", rs.Name, migErr))
					fmt.Printf("     [⚠️ ] Schema reconciliation warning: %v\n", migErr)
				}
			}
			// Data seeding on cached resource — loop over all DATA blocks.
			if ce.apiKey != "" && cached.ConnectionString != "" {
				for _, data := range rs.Datas {
					ce.addLog(fmt.Sprintf("[⚡] Data Seeding (cached): seeding rows into '%s' for '%s'...", data.InsertInto, rs.Name))
					if seedErr := RunSeed(cached.ConnectionString, data, rs.Name); seedErr != nil {
						ce.addLog(fmt.Sprintf("[⚠️ ] Data seeding warning for '%s': %v", rs.Name, seedErr))
						fmt.Printf("     [⚠️ ] Data seeding warning: %v\n", seedErr)
					}
				}
			}
			continue
		}

		// ── Fresh provision via Neon API ──────────────────────────────────────
		// Lazily resolve the org ID the first time we actually need a live call.
		if !orgResolved {
			if resolveErr := ce.resolveOrgID(); resolveErr != nil {
				return resolveErr
			}
			orgResolved = true
		}

		result, provErr := ce.provisionNeonProject(rs)
		if provErr != nil {
			return fmt.Errorf("provision '%s': %w", rs.Name, provErr)
		}

		// ── Persist to lock file ──────────────────────────────────────────────
		lr := LockResource{
			Provider:         "neon",
			Type:             "postgres",
			ProjectID:        result.ProjectID,
			ConnectionString: result.ConnectionString,
			Host:             result.Host,
			PoolerHost:       result.PoolerHost,
			Database:         result.Database,
			User:             result.User,
			Region:           propValue(rs, "region"), // FIX: persist region so cached path can display it
			Status:           "active",
		}
		if writeErr := ce.lockFile.UpsertResource(rs.Name, lr); writeErr != nil {
			// Non-fatal: log the warning but continue — the deploy succeeded.
			ce.addLog(fmt.Sprintf("[⚠️ ] Could not update %s: %v", LockFilePath, writeErr))
		} else {
			ce.addLog(fmt.Sprintf("[🔒] State saved → %s (resource: %s)", LockFilePath, rs.Name))
		}

		// ── Auto-Migration: execute SCHEMA block on the live database ──────
		// Only runs in live mode (real API key present) to avoid dialling
		// simulation hostnames that don't resolve.
		if schemas := collectSchemas(rs); len(schemas) > 0 && ce.apiKey != "" {
			ce.addLog(fmt.Sprintf("[⚡] Auto-Migration: running schema for resource '%s'...", rs.Name))
			if migErr := RunMigrations(result.ConnectionString, schemas, rs.Name); migErr != nil {
				// Non-fatal: log but continue — table can be created manually.
				ce.addLog(fmt.Sprintf("[⚠️ ] Auto-migration warning for '%s': %v", rs.Name, migErr))
				fmt.Printf("     [⚠️ ] Auto-migration warning: %v\n", migErr)
			}
		}

		// ── Data Seeding: loop over all DATA blocks on the live database ──────
		if ce.apiKey != "" {
			for _, data := range rs.Datas {
				ce.addLog(fmt.Sprintf("[⚡] Data Seeding: seeding rows into '%s' for resource '%s'...", data.InsertInto, rs.Name))
				if seedErr := RunSeed(result.ConnectionString, data, rs.Name); seedErr != nil {
					ce.addLog(fmt.Sprintf("[⚠️ ] Data seeding warning for '%s': %v", rs.Name, seedErr))
					fmt.Printf("     [⚠️ ] Data seeding warning: %v\n", seedErr)
				}
			}
		}

		ce.Results = append(ce.Results, *result)
		provisioned++
	}

	return nil
}

// provisionNeonProject calls the Neon API to create a single project.
func (ce *CloudEmitter) provisionNeonProject(rs *parser.ResourceStatement) (*CloudResult, error) {
	// Convert the Sajon resource name to a URL-safe Neon project name.
	projectName := "sajon-" + strings.ReplaceAll(rs.Name, "_", "-")

	// Map the Sajon region string to a Neon region ID.
	regionID := mapToNeonRegion(propValue(rs, "region"))

	// ── Build JSON request body ───────────────────────────────────────────
	reqBody := neonCreateRequest{
		Project: neonProjectSpec{
			Name:     projectName,
			RegionID: regionID,
			OrgID:    ce.orgID,
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}

	// ── Build HTTP request ────────────────────────────────────────────────
	req, err := http.NewRequest(http.MethodPost, neonAPIBase+"/projects", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+ce.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	// ── Execute ───────────────────────────────────────────────────────────
	client := &http.Client{Timeout: neonAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP POST: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// ── Validate HTTP status ──────────────────────────────────────────────
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		// Surface a helpful snippet of the error body without leaking the key.
		snippet := string(respBytes)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return nil, fmt.Errorf("Neon API %d: %s", resp.StatusCode, snippet)
	}

	// ── Parse JSON response ───────────────────────────────────────────────
	var neonResp neonCreateResponse
	if err := json.Unmarshal(respBytes, &neonResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(neonResp.ConnectionURIs) == 0 {
		return nil, fmt.Errorf("Neon returned no connection URIs for project '%s'", projectName)
	}

	// Primary connection (index 0) is the direct host; use the pooler URI
	// for production workloads if present.
	conn   := neonResp.ConnectionURIs[0]
	params := conn.ConnectionParameters

	result := &CloudResult{
		ResourceName:     rs.Name,
		ProjectID:        neonResp.Project.ID,
		ProjectName:      neonResp.Project.Name,
		Region:           propValue(rs, "region"), // store the user-facing region string
		Host:             params.Host,
		PoolerHost:       params.PoolerHost,
		Database:         params.Database,
		User:             params.Role,
		Password:         params.Password,
		ConnectionString: conn.ConnectionURI,
		FromCache:        false,
	}

	ce.addLog(fmt.Sprintf(
		"[%s] %-14s  →  Project: %-28s  Host: %s",
		rs.Kind, rs.Name, result.ProjectID, result.Host,
	))

	return result, nil
}

// ── Neon API wire types ───────────────────────────────────────────────────────
// These structs model only the fields Sajon needs; the full Neon API schema
// has many more optional fields that are safely ignored here.

type neonCreateRequest struct {
	Project neonProjectSpec `json:"project"`
}

type neonProjectSpec struct {
	Name     string `json:"name"`
	RegionID string `json:"region_id,omitempty"`
	OrgID    string `json:"org_id,omitempty"`
}

type neonCreateResponse struct {
	Project struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"project"`
	ConnectionURIs []struct {
		ConnectionURI        string `json:"connection_uri"`
		ConnectionParameters struct {
			Database   string `json:"database"`
			Host       string `json:"host"`
			PoolerHost string `json:"pooler_host"`
			Password   string `json:"password"`
			Role       string `json:"role"`
		} `json:"connection_parameters"`
	} `json:"connection_uris"`
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// mapToNeonRegion converts a Sajon region string (AWS-style) to the
// corresponding Neon region identifier.  Falls back to "aws-us-east-2" which
// is always available on Neon's free tier.
func mapToNeonRegion(sajRegion string) string {
	regionMap := map[string]string{
		"us-east-1":      "aws-us-east-2",   // Neon has no us-east-1; nearest is us-east-2
		"us-east-2":      "aws-us-east-2",
		"us-west-2":      "aws-us-west-2",
		"eu-west-1":      "aws-eu-central-1",
		"eu-central-1":   "aws-eu-central-1",
		"ap-southeast-1": "aws-ap-southeast-1",
	}
	if neonRegion, ok := regionMap[sajRegion]; ok {
		return neonRegion
	}
	return "aws-us-east-2" // safe default (free-tier eligible)
}

// resolveOrgID fetches the first organization ID from the Neon API if ce.orgID is empty.
func (ce *CloudEmitter) resolveOrgID() error {
	if ce.orgID != "" {
		ce.addLog(fmt.Sprintf("  Neon using explicit organisation ID: %s (from NEON_ORG_ID)", ce.orgID))
		return nil
	}

	req, err := http.NewRequest(http.MethodGet, neonAPIBase+"/users/me/organizations", nil)
	if err != nil {
		return fmt.Errorf("build request to fetch organizations: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+ce.apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: neonAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("fetch organizations HTTP GET: %w", err)
	}
	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read organizations response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(respBytes)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return fmt.Errorf("Neon API GET /users/me/organizations returned %d: %s", resp.StatusCode, snippet)
	}

	var orgsResp struct {
		Organizations []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"organizations"`
	}
	if err := json.Unmarshal(respBytes, &orgsResp); err != nil {
		return fmt.Errorf("parse organizations response: %w", err)
	}

	if len(orgsResp.Organizations) == 0 {
		return fmt.Errorf("no Neon organizations found for this account")
	}

	ce.orgID = orgsResp.Organizations[0].ID
	ce.addLog(fmt.Sprintf("  Neon /v2/users/me/organizations →  mode: live  org: %s (%s)", ce.orgID, orgsResp.Organizations[0].Name))
	return nil
}

// addLog appends a provisioning event message to the CloudEmitter's log.
func (ce *CloudEmitter) addLog(msg string) {
	ce.Log = append(ce.Log, msg)
}
