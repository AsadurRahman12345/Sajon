// supabase_emitter.go — Sajon Supabase Provisioner
//
// Implements the real Supabase Management API provisioning pipeline.
// When SUPABASE_ACCESS_TOKEN is set, each supabase ResourceStatement in the
// AST results in a live Supabase project being created and its connection
// string printed to the terminal.
//
// Real Supabase Management API endpoint:
//   POST https://api.supabase.com/v1/projects
//   Authorization: Bearer <SUPABASE_ACCESS_TOKEN>
//
// Pipeline steps:
//   1. Authenticate   — GET /v1/organizations → real org_id
//   2. Resolve org    — pick first org from list
//   3. Create project — POST /v1/projects with name + db_pass + region
//   4. Wait ready     — poll GET /v1/projects/{ref} until status = "ACTIVE_HEALTHY"
//   5. Assemble DSN   — build full postgresql:// connection string
//
// Simulation fallback: when accessToken is empty, realistic fake output is
// produced without any network calls — identical behaviour to before.
package emitter

import (
	"bytes"
	crand "crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	mrand "math/rand"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sajon/pkg/parser"
)

// ── Supabase API constants ────────────────────────────────────────────────────

const (
	supabaseAPIBase = "https://api.supabase.com/v1"
	// FIX #5: Increased from 90s to 5 minutes — real Supabase projects commonly
	// take 2–4 minutes to become ACTIVE_HEALTHY on the first provision.
	supabaseAPITimeout    = 60 * time.Second  // HTTP client read timeout per request
	supabaseProjectTimeout = 5 * time.Minute  // total deadline waiting for ACTIVE_HEALTHY
)

// ── Result type ───────────────────────────────────────────────────────────────

// SupabaseResult holds all provisioned project details returned by the
// Supabase Management API.  Fields map 1-to-1 with the API response body.
type SupabaseResult struct {
	ResourceName     string // Sajon source name         (e.g. "rentic_staging")
	ProjectRef       string // Supabase project ref      (e.g. "abcdefghijklmnop")
	ProjectName      string // project name sent to API  (e.g. "sajon-rentic-staging")
	OrgID            string // Supabase organisation ID
	Region           string // Supabase region           (e.g. "ap-southeast-1")
	Host             string // DB direct host            (e.g. "db.abcdef.supabase.co")
	PoolerHost       string // Supavisor pooler host     (e.g. "aws-0-ap-southeast-1.pooler.supabase.com")
	Database         string // default DB name           (always "postgres")
	User             string // default role              (always "postgres")
	Password         string // generated DB password (sensitive — never log fully)
	ConnectionString string // full postgresql:// DSN with sslmode=require
	DashboardURL     string // https://app.supabase.com/project/<ref>
}

// ── SupabaseEmitter struct ───────────────────────────────────────────────────

// SupabaseEmitter provisions Supabase projects for every ResourceStatement
// whose provider is "supabase".
type SupabaseEmitter struct {
	program     *parser.Program
	accessToken string // from SUPABASE_ACCESS_TOKEN env var
	orgID       string // from SUPABASE_ORG_ID env var (auto-detected when empty)
	client      *http.Client
	Results     []SupabaseResult
	Log         []string
}

// NewSupabase creates a SupabaseEmitter.
// accessToken comes from SUPABASE_ACCESS_TOKEN; orgID from SUPABASE_ORG_ID.
// Both may be empty — the emitter runs in simulation mode when credentials
// are absent, producing realistic output without making real API calls.
func NewSupabase(p *parser.Program, accessToken, orgID string) *SupabaseEmitter {
	return &SupabaseEmitter{
		program:     p,
		accessToken: accessToken,
		orgID:       orgID,
		client:      &http.Client{Timeout: supabaseAPITimeout},
	}
}

// ── Core provisioning ─────────────────────────────────────────────────────────

// ProvisionAll walks the AST and deploys every ResourceStatement whose
// provider is "supabase".
//
// STATE MANAGEMENT: Before calling the Supabase API for any resource, the
// method checks sajon.lock.  Active (already-provisioned) resources are
// loaded from the lock and returned without touching the API — making
// repeated 'sajon up' runs fully idempotent.
//
// PERSISTENCE: After a successful provision the result is immediately
// persisted to sajon.lock via UpsertResource so that:
//   - 'sajon down' can locate the project_id for the DELETE API call.
//   - The Orphan Guard can detect renames across future 'sajon up' runs.
func (se *SupabaseEmitter) ProvisionAll() error {
	// ── Step 0: Load the lock file ─────────────────────────────────────────
	lf, err := ReadLockFile()
	if err != nil {
		return fmt.Errorf("state file: %w", err)
	}

	provisioned := 0
	for _, stmt := range se.program.Statements {
		rs, ok := stmt.(*parser.ResourceStatement)
		if !ok {
			continue
		}
		if propValue(rs, "provider") != "supabase" {
			continue
		}

		// ── Lock-file pre-check: skip already-active resources ─────────────
		// This makes 'sajon up' idempotent — a resource that was already
		// successfully provisioned will not trigger a second API call.
		if cached, found := lf.GetActiveResource(rs.Name); found {
			se.addLog(fmt.Sprintf(
				"[i] Resource '%s' is already live (project_id: %s) — restored from sajon.lock",
				rs.Name, cached.ProjectID,
			))
			fmt.Printf("     [lock]  '%s'  already live — restored from sajon.lock (skipping API call)\n", rs.Name)
			result := SupabaseResult{
				ResourceName:     rs.Name,
				ProjectRef:       cached.ProjectID,
				ProjectName:      "sajon-" + strings.ReplaceAll(rs.Name, "_", "-"),
				Region:           cached.Host,
				Host:             cached.Host,
				PoolerHost:       cached.PoolerHost,
				Database:         cached.Database,
				User:             cached.User,
				Password:         "", // not stored in lock for security
				ConnectionString: cached.ConnectionString,
			}
			se.Results = append(se.Results, result)
			provisioned++
			continue
		}

		// ── Fresh provision via Supabase API ───────────────────────────────
		result, err := se.deployToSupabase(rs)
		if err != nil {
			return fmt.Errorf("Supabase provision '%s': %w", rs.Name, err)
		}

		// ── Persist to lock file ───────────────────────────────────────────
		// Mirror the pattern from cloud_emitter.go (Neon).
		// Non-fatal: log the warning but continue — the deploy succeeded.
		lr := LockResource{
			Provider:         "supabase",
			Type:             "postgres",
			ProjectID:        result.ProjectRef,
			ConnectionString: result.ConnectionString,
			Host:             result.Host,
			PoolerHost:       result.PoolerHost,
			Database:         result.Database,
			User:             result.User,
			Status:           "active",
		}
		if writeErr := lf.UpsertResource(rs.Name, lr); writeErr != nil {
			se.addLog(fmt.Sprintf("[WARN] Could not update %s: %v", LockFilePath, writeErr))
		} else {
			se.addLog(fmt.Sprintf("[lock] State saved -> %s (resource: %s)", LockFilePath, rs.Name))
			fmt.Printf("     [lock]  sajon.lock updated — '%s' state persisted.\n", rs.Name)
		}

		// ── Auto-Migration: execute SCHEMA block on the live database ──────
		// Only runs in live mode (real access token present) so that
		// simulation runs never attempt to dial a non-existent host.
		if schemas := collectSchemas(rs); len(schemas) > 0 && se.accessToken != "" {
			se.addLog(fmt.Sprintf("[⚡] Auto-Migration: running schema for resource '%s'...", rs.Name))
			if migErr := RunMigrations(result.ConnectionString, schemas, rs.Name); migErr != nil {
				// Non-fatal: log but continue — table can be created manually.
				se.addLog(fmt.Sprintf("[⚠️ ] Auto-migration warning for '%s': %v", rs.Name, migErr))
				fmt.Printf("     [⚠️ ] Auto-migration warning: %v\n", migErr)
			}
		}

		se.Results = append(se.Results, *result)
		provisioned++
	}
	if provisioned == 0 {
		se.addLog("No Supabase resources found — nothing provisioned.")
	}
	return nil
}

// deployToSupabase runs the full 5-step Supabase provisioning pipeline for
// one ResourceStatement. When accessToken is set, makes real API calls.
// When absent, falls back to realistic simulation.
func (se *SupabaseEmitter) deployToSupabase(rs *parser.ResourceStatement) (*SupabaseResult, error) {
	projectName := "sajon-" + strings.ReplaceAll(rs.Name, "_", "-")
	region := propValue(rs, "region")
	if region == "" {
		region = "ap-southeast-1" // Supabase default Asia region
	}

	isLive := se.accessToken != ""

	// ── Step 1 — Authenticate ─────────────────────────────────────────────
	se.stepLog(rs, "📡", "Step 1/5", "Authenticating with SUPABASE_ACCESS_TOKEN...")
	orgID, err := se.stepAuthenticate(isLive)
	if err != nil {
		return nil, err
	}
	if se.orgID != "" {
		orgID = se.orgID // honour explicit override from env var
	}

	// ── Step 2 — Resolve organisation ─────────────────────────────────────
	se.stepLog(rs, "🏢", "Step 2/5", fmt.Sprintf("Resolved organisation → %s", orgID))

	// ── Step 3 — Create project ────────────────────────────────────────────
	se.stepLog(rs, "🚀", "Step 3/5", fmt.Sprintf("Spinning up Supabase project '%s' in region: %s...", projectName, region))
	projectRef, dbPassword, err := se.stepCreateProject(projectName, region, orgID, isLive)
	if err != nil {
		return nil, err
	}

	// ── Step 4 — Wait for ACTIVE_HEALTHY ──────────────────────────────────
	se.stepLog(rs, "⏳", "Step 4/5", "Waiting for project status: ACTIVE_HEALTHY...")
	if err := se.stepWaitReady(projectRef, isLive); err != nil {
		return nil, err
	}

	// ── Step 5 — Assemble connection string ────────────────────────────────
	host := fmt.Sprintf("db.%s.supabase.co", projectRef)
	poolerHost := fmt.Sprintf("aws-0-%s.pooler.supabase.com", region)
	// FIX: Percent-encode the password so that special characters (@ ! # $ etc.)
	// generated by crypto/rand never break the postgresql:// URL parser.
	// Without encoding, an '@' in the password is misread as the
	// user:password@host boundary, causing "invalid port" parse errors.
	encodedPassword := url.QueryEscape(dbPassword)
	connStr := fmt.Sprintf(
		"postgresql://postgres:%s@%s:5432/postgres?sslmode=require",
		encodedPassword, host,
	)
	dashURL := fmt.Sprintf("https://app.supabase.com/project/%s", projectRef)

	se.stepLog(rs, "✅", "Step 5/5", "Project ACTIVE_HEALTHY — connection string assembled.")
	se.addLog(fmt.Sprintf(
		"[%s] %-16s  →  Supabase %-18s  Region: %s  Host: %s",
		rs.Kind, rs.Name, projectRef, region, host,
	))

	return &SupabaseResult{
		ResourceName:     rs.Name,
		ProjectRef:       projectRef,
		ProjectName:      projectName,
		OrgID:            orgID,
		Region:           region,
		Host:             host,
		PoolerHost:       poolerHost,
		Database:         "postgres",
		User:             "postgres",
		Password:         dbPassword,
		ConnectionString: connStr,
		DashboardURL:     dashURL,
	}, nil
}

// ── Step implementations (Real API calls) ─────────────────────────────────────

// stepAuthenticate calls GET /v1/organizations to validate the token and
// return the first organisation's ID. Falls back to simulation if no token.
func (se *SupabaseEmitter) stepAuthenticate(isLive bool) (string, error) {
	if !isLive {
		// ── Simulation fallback ────────────────────────────────────────────
		time.Sleep(320 * time.Millisecond)
		orgID := fmt.Sprintf("org-%08x", mrand.Uint32())
		se.addLog(fmt.Sprintf(
			"  Supabase /v1/profile     →  mode: simulation  token: <not set>"))
		se.addLog(fmt.Sprintf(
			"  Supabase /v1/organizations →  default org: %s", orgID))
		return orgID, nil
	}

	// ── Real API call ──────────────────────────────────────────────────────
	req, err := http.NewRequest(http.MethodGet, supabaseAPIBase+"/organizations", nil)
	if err != nil {
		return "", fmt.Errorf("build orgs request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+se.accessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := se.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET /v1/organizations: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read orgs response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return "", fmt.Errorf("Supabase /v1/organizations %d: %s", resp.StatusCode, snippet)
	}

	// Response is a JSON array of org objects: [{id, name}, ...]
	var orgs []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &orgs); err != nil {
		return "", fmt.Errorf("parse orgs: %w", err)
	}
	if len(orgs) == 0 {
		return "", fmt.Errorf("no Supabase organisations found for this token")
	}

	orgID := orgs[0].ID
	tokenDisplay := se.accessToken
	if len(tokenDisplay) > 8 {
		tokenDisplay = tokenDisplay[:8] + "…"
	}
	se.addLog(fmt.Sprintf(
		"  Supabase /v1/organizations →  mode: live  token: %s  org: %s (%s)",
		tokenDisplay, orgID, orgs[0].Name))
	return orgID, nil
}

// stepCreateProject calls POST /v1/projects to create the Supabase project.
// Returns the project ref and the generated database password.
func (se *SupabaseEmitter) stepCreateProject(name, region, orgID string, isLive bool) (string, string, error) {
	// FIX #6: Generate a cryptographically secure DB password using crypto/rand.
	// math/rand is NOT cryptographically secure; crypto/rand is.
	const pwdChars = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$"
	pwdBytes := make([]byte, 24)
	cryptBytes := make([]byte, 24)
	if _, err := crand.Read(cryptBytes); err != nil {
		// Fallback to math/rand only if crypto/rand fails (extremely unlikely)
		r := mrand.New(mrand.NewSource(time.Now().UnixNano()))
		for i := range pwdBytes {
			pwdBytes[i] = pwdChars[r.Intn(len(pwdChars))]
		}
	} else {
		for i, v := range cryptBytes {
			pwdBytes[i] = pwdChars[int(v)%len(pwdChars)]
		}
	}
	dbPassword := string(pwdBytes)

	if !isLive {
		// ── Simulation fallback ──────────────────────────────────────────────
		time.Sleep(780 * time.Millisecond)
		const refChars = "abcdefghijklmnopqrstuvwxyz"
		refBytes := make([]byte, 16)
		// Use math/rand for simulation (non-security-sensitive)
		r := mrand.New(mrand.NewSource(time.Now().UnixNano()))
		for i := range refBytes {
			refBytes[i] = refChars[r.Intn(len(refChars))]
		}
		projectRef := string(refBytes)
		se.addLog(fmt.Sprintf(
			"  POST /v1/projects        →  name: %-30s  region: %s", name, region))
		se.addLog(fmt.Sprintf(
			"  POST /v1/projects        →  ref:  %-30s  org: %s", projectRef, orgID))
		return projectRef, dbPassword, nil
	}

	// ── Real API call ──────────────────────────────────────────────────────
	// NOTE: Supabase Management API requires "organization_id" (NOT "org_id").
	// The "plan" field must NOT be sent — it causes 403 "Resource context not found"
	// with personal access tokens; the plan is inferred from the org's subscription.
	payload := map[string]string{
		"name":            name,
		"region":          region,
		"organization_id": orgID,
		"db_pass":         dbPassword,
	}
	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", fmt.Errorf("marshal create project: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, supabaseAPIBase+"/projects", bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", fmt.Errorf("build create project request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+se.accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := se.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("POST /v1/projects: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read create project response: %w", err)
	}
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		snippet := string(respBody)
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return "", "", fmt.Errorf("Supabase POST /v1/projects %d: %s", resp.StatusCode, snippet)
	}

	// Parse response to get project ref (id field)
	var proj struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Region string `json:"region"`
	}
	if err := json.Unmarshal(respBody, &proj); err != nil {
		return "", "", fmt.Errorf("parse create project response: %w", err)
	}

	se.addLog(fmt.Sprintf(
		"  POST /v1/projects        →  name: %-30s  region: %s", name, region))
	se.addLog(fmt.Sprintf(
		"  POST /v1/projects        →  ref:  %-30s  org: %s  status: %s", proj.ID, orgID, proj.Status))
	return proj.ID, dbPassword, nil
}

// stepWaitReady polls GET /v1/projects/{ref} until status is ACTIVE_HEALTHY.
// Falls back to simulated status progression when not live.
func (se *SupabaseEmitter) stepWaitReady(projectRef string, isLive bool) error {
	if !isLive {
		// ── Simulation fallback ────────────────────────────────────────────
		statuses := []string{"COMING_UP", "RESTORING", "UPGRADING", "ACTIVE_HEALTHY"}
		for i, status := range statuses {
			time.Sleep(time.Duration(120+i*90) * time.Millisecond)
			se.addLog(fmt.Sprintf(
				"  GET /v1/projects/%-18s →  status: %s", projectRef, status))
		}
		return nil
	}

	// ── Real polling loop ──────────────────────────────────────────────────
	url := fmt.Sprintf("%s/projects/%s", supabaseAPIBase, projectRef)
	// FIX #5: Use the configurable supabaseProjectTimeout (5 min) instead of
	// a hardcoded 90-second limit that often fires before the project is ready.
	deadline := time.Now().Add(supabaseProjectTimeout)
	attempt := 0

	for time.Now().Before(deadline) {
		attempt++
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("build status request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+se.accessToken)
		req.Header.Set("Accept", "application/json")

		resp, err := se.client.Do(req)
		if err != nil {
			return fmt.Errorf("GET /v1/projects/%s: %w", projectRef, err)
		}

		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		var proj struct {
			Status string `json:"status"`
		}
		json.Unmarshal(body, &proj)

		se.addLog(fmt.Sprintf(
			"  GET /v1/projects/%-18s →  status: %s  (poll #%d)", projectRef, proj.Status, attempt))

		if proj.Status == "ACTIVE_HEALTHY" {
			return nil
		}

		// Exponential backoff: 3s, 5s, 7s, 10s, 10s...
		wait := time.Duration(3+attempt*2) * time.Second
		if wait > 10*time.Second {
			wait = 10 * time.Second
		}
		time.Sleep(wait)
	}

	return fmt.Errorf("Supabase project '%s' did not become ACTIVE_HEALTHY within %s", projectRef, supabaseProjectTimeout)
}

// ── Helpers ───────────────────────────────────────────────────────────────────

// stepLog prints a formatted phase header line to stdout for live progress.
func (se *SupabaseEmitter) stepLog(rs *parser.ResourceStatement, icon, label, desc string) {
	fmt.Printf("     %s  %s  [%s]  %s\n", icon, label, rs.Name, desc)
}

// addLog appends an event message to the SupabaseEmitter log.
func (se *SupabaseEmitter) addLog(msg string) {
	se.Log = append(se.Log, msg)
}
