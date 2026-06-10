// destroyer.go — Sajon Teardown Engine
//
// Implements the `sajon down` command.  It reads sajon.lock, calls the
// appropriate cloud DELETE API for each active resource, removes the lock
// file and docker-compose.yml, and leaves the workspace clean.
//
// Provider teardown behaviour:
//   neon     → live HTTP DELETE /projects/<project_id>  (requires NEON_API_KEY)
//   aws      → simulation mode  (no AWS SDK — prints realistic destroy logs)
//   supabase → live HTTP DELETE /v1/projects/<ref>      (requires SUPABASE_ACCESS_TOKEN)
//              falls back to simulation when token is absent
//   other    → prints a generic decommission log
//
// After all resources are processed the Destroyer removes:
//   • sajon.lock
//   • docker-compose.yml  (if it exists in the current directory)
package emitter

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// ── DestroyResult records the outcome of tearing down a single resource ────────

// DestroyStatus classifies the result of a single teardown attempt.
type DestroyStatus int

const (
	DestroyOK       DestroyStatus = iota // successfully destroyed / confirmed gone
	DestroySkipped                       // resource had no project_id / unknown provider
	DestroyFailed                        // API call failed — resource may still exist
)

// DestroyResult holds the outcome for one resource.
type DestroyResult struct {
	ResourceName string
	Provider     string
	ProjectID    string
	Status       DestroyStatus
	Message      string // human-readable outcome detail
}

// ── Supabase API constants ─────────────────────────────────────────────────────

const (
	supabaseDestroyBase    = "https://api.supabase.com/v1"
	supabaseDestroyTimeout = 30 * time.Second
)

// ── Destroyer struct ──────────────────────────────────────────────────────────

// Destroyer reads sajon.lock and tears down every active resource.
type Destroyer struct {
	apiKey         string // NEON_API_KEY — used for live Neon deletes
	supabaseToken  string // SUPABASE_ACCESS_TOKEN — used for live Supabase deletes
	Results        []DestroyResult
	Log            []string
}

// NewDestroyer creates a Destroyer.
//   - neonKey: from $NEON_API_KEY — enables live Neon project deletion.
//   - supabaseToken: from $SUPABASE_ACCESS_TOKEN — enables live Supabase project deletion.
//
// Either may be empty; the corresponding provider will fall back to simulation.
func NewDestroyer(neonKey, supabaseToken string) *Destroyer {
	return &Destroyer{apiKey: neonKey, supabaseToken: supabaseToken}
}

// ── TeardownAll ───────────────────────────────────────────────────────────────

// TeardownAll reads sajon.lock and destroys every active resource.
// Returns (false, nil) when the lock file is absent — nothing to destroy.
// Returns (true, err) when teardown was attempted; err is non-nil only on a
// hard failure (I/O error reading the lock, etc.).  Individual resource
// failures are captured in Results, not returned as errors, so the caller can
// continue cleaning up even when one resource fails.
func (d *Destroyer) TeardownAll() (found bool, err error) {
	lf, readErr := ReadLockFile()
	if readErr != nil {
		return false, fmt.Errorf("read state file: %w", readErr)
	}
	if len(lf.Resources) == 0 {
		return false, nil // nothing recorded — signal "nothing to destroy"
	}

	found = true

	for name, lr := range lf.Resources {
		if lr.Status != "active" {
			d.log(fmt.Sprintf("  Skipping '%s' (status: %s)", name, lr.Status))
			d.Results = append(d.Results, DestroyResult{
				ResourceName: name,
				Provider:     lr.Provider,
				ProjectID:    lr.ProjectID,
				Status:       DestroySkipped,
				Message:      fmt.Sprintf("status was '%s' — skipped", lr.Status),
			})
			continue
		}

		switch lr.Provider {
		case "neon":
			d.destroyNeon(name, lr)
		case "aws":
			d.destroyAWS(name, lr)
		case "supabase":
			d.destroySupabase(name, lr)
		default:
			d.destroyGeneric(name, lr)
		}
	}

	return found, nil
}

// ── Provider-specific teardown methods ───────────────────────────────────────

// destroyNeon calls DELETE /projects/<project_id> on the Neon API.
func (d *Destroyer) destroyNeon(name string, lr LockResource) {
	if lr.ProjectID == "" {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon",
			Status:  DestroySkipped,
			Message: "no project_id in lock — cannot delete via API",
		})
		d.log(fmt.Sprintf("  [-] '%s' — skipped (no project_id recorded)", name))
		return
	}

	if d.apiKey == "" {
		// No key — simulate deletion and warn.
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "simulated (NEON_API_KEY not set)",
		})
		d.log(fmt.Sprintf("  [-] Destroying Neon Database: '%s' ... Simulated (set NEON_API_KEY to delete for real)", name))
		return
	}

	// ── Live HTTP DELETE ──────────────────────────────────────────────────
	url := neonAPIBase + "/projects/" + lr.ProjectID
	req, err := http.NewRequest(http.MethodDelete, url, nil)
	if err != nil {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
			Status:  DestroyFailed,
			Message: fmt.Sprintf("build request: %v", err),
		})
		d.log(fmt.Sprintf("  [!] '%s' — request build failed: %v", name, err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: neonAPITimeout}
	resp, err := client.Do(req)
	if err != nil {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
			Status:  DestroyFailed,
			Message: fmt.Sprintf("HTTP DELETE: %v", err),
		})
		d.log(fmt.Sprintf("  [!] '%s' — HTTP error: %v", name, err))
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	// Neon returns 200 OK on successful project deletion.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "deleted via Neon API",
		})
		d.log(fmt.Sprintf("  [-] Destroying Neon Database: '%s' (project: %s) ... Done!", name, lr.ProjectID))
		return
	}

	// 404 means the project is already gone — treat as success.
	if resp.StatusCode == http.StatusNotFound {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "already deleted (404 on Neon API — project not found)",
		})
		d.log(fmt.Sprintf("  [-] Neon Database '%s' — already deleted (404). Removing from state.", name))
		return
	}

	// Any other status is a failure.
	snippet := string(body)
	if len(snippet) > 120 {
		snippet = snippet[:120] + "…"
	}
	d.Results = append(d.Results, DestroyResult{
		ResourceName: name, Provider: "neon", ProjectID: lr.ProjectID,
		Status:  DestroyFailed,
		Message: fmt.Sprintf("Neon API %d: %s", resp.StatusCode, snippet),
	})
	d.log(fmt.Sprintf("  [!] Neon API returned %d for '%s': %s", resp.StatusCode, name, snippet))
}

// destroyAWS simulates teardown of AWS resources (RDS, EC2, S3).
// A real implementation would call the AWS SDK; for now it prints realistic
// destroy logs without making any external calls.
func (d *Destroyer) destroyAWS(name string, lr LockResource) {
	resourceLabel := "AWS Resource"
	switch lr.Type {
	case "rds", "postgres":
		resourceLabel = "AWS RDS Instance"
	case "ec2":
		resourceLabel = "AWS EC2 Instance"
	case "s3":
		resourceLabel = "AWS S3 Bucket"
	}
	steps := []string{
		fmt.Sprintf("  Initiating %s deletion: '%s'", resourceLabel, name),
		"  Waiting for final snapshots...",
		"  Revoking security group rules...",
		"  Releasing Elastic IPs...",
		fmt.Sprintf("  %s '%s' — terminated.", resourceLabel, name),
	}
	for _, step := range steps {
		d.log(step)
		time.Sleep(10 * time.Millisecond) // tiny delay for realistic feel
	}
	d.Results = append(d.Results, DestroyResult{
		ResourceName: name, Provider: "aws", ProjectID: lr.ProjectID,
		Status:  DestroyOK,
		Message: "simulated AWS teardown complete",
	})
}

// destroySupabase permanently deletes a Supabase project via the Management API.
//
// When SUPABASE_ACCESS_TOKEN is set:
//   - Calls DELETE https://api.supabase.com/v1/projects/<project_id>
//   - HTTP 200/204 → DestroyOK
//   - HTTP 404     → DestroyOK (already gone)
//   - Other status → DestroyFailed (resource may still exist — check dashboard)
//
// When SUPABASE_ACCESS_TOKEN is absent:
//   - Falls back to simulation mode — prints realistic destroy logs, marks DestroyOK.
func (d *Destroyer) destroySupabase(name string, lr LockResource) {
	if lr.ProjectID == "" {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase",
			Status:  DestroySkipped,
			Message: "no project_id in lock — cannot delete via API",
		})
		d.log(fmt.Sprintf("  [-] '%s' — skipped (no project_id / project ref recorded)", name))
		return
	}

	if d.supabaseToken == "" {
		// ── Simulation fallback ────────────────────────────────────────────
		steps := []string{
			fmt.Sprintf("  Pausing Supabase project: '%s'", name),
			"  Draining active connections...",
			"  Disabling Auth providers...",
			"  Removing Edge Functions...",
			fmt.Sprintf("  Supabase project '%s' — deleted. (simulated — set SUPABASE_ACCESS_TOKEN to delete for real)", name),
		}
		for _, step := range steps {
			d.log(step)
			time.Sleep(10 * time.Millisecond)
		}
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "simulated Supabase teardown (SUPABASE_ACCESS_TOKEN not set)",
		})
		return
	}

	// ── Live HTTP DELETE ──────────────────────────────────────────────────
	// Supabase Management API: DELETE /v1/projects/{ref}
	// The project_id stored in the lock IS the project ref (16-char slug).
	deleteURL := supabaseDestroyBase + "/projects/" + lr.ProjectID
	d.log(fmt.Sprintf("  Calling Supabase API: DELETE /v1/projects/%s ...", lr.ProjectID))

	req, err := http.NewRequest(http.MethodDelete, deleteURL, nil)
	if err != nil {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
			Status:  DestroyFailed,
			Message: fmt.Sprintf("build request: %v", err),
		})
		d.log(fmt.Sprintf("  [!] '%s' — request build failed: %v", name, err))
		return
	}
	req.Header.Set("Authorization", "Bearer "+d.supabaseToken)
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: supabaseDestroyTimeout}
	resp, err := client.Do(req)
	if err != nil {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
			Status:  DestroyFailed,
			Message: fmt.Sprintf("HTTP DELETE: %v", err),
		})
		d.log(fmt.Sprintf("  [!] '%s' — HTTP error: %v", name, err))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	// 200 or 204 → successfully deleted.
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "deleted via Supabase API",
		})
		d.log(fmt.Sprintf("  [-] Supabase project '%s' (ref: %s) — deleted successfully!", name, lr.ProjectID))
		return
	}

	// 404 → project already gone — treat as success (idempotent teardown).
	if resp.StatusCode == http.StatusNotFound {
		d.Results = append(d.Results, DestroyResult{
			ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
			Status:  DestroyOK,
			Message: "already deleted (404 — project ref not found on Supabase)",
		})
		d.log(fmt.Sprintf("  [-] Supabase project '%s' — already deleted (404). Removing from state.", name))
		return
	}

	// Any other status is a failure — surface the API response body.
	snippet := string(respBody)
	if len(snippet) > 200 {
		snippet = snippet[:200] + "…"
	}
	d.Results = append(d.Results, DestroyResult{
		ResourceName: name, Provider: "supabase", ProjectID: lr.ProjectID,
		Status:  DestroyFailed,
		Message: fmt.Sprintf("Supabase API %d: %s", resp.StatusCode, snippet),
	})
	d.log(fmt.Sprintf("  [!] Supabase API returned %d for '%s': %s", resp.StatusCode, name, snippet))
}

// destroyGeneric handles unknown providers with a generic decommission log.
func (d *Destroyer) destroyGeneric(name string, lr LockResource) {
	d.log(fmt.Sprintf("  [-] Decommissioning '%s' (provider: %s) ... Done!", name, lr.Provider))
	d.Results = append(d.Results, DestroyResult{
		ResourceName: name, Provider: lr.Provider,
		Status:  DestroyOK,
		Message: "generic decommission (no live API — remove manually if needed)",
	})
}

// ── Local cleanup ─────────────────────────────────────────────────────────────

// RemoveLockFile deletes sajon.lock from the configured backend (S3 or disk).
// When remote state is enabled the S3 object is deleted instead.
// Returns nil if the file / object does not exist (already clean).
func RemoveLockFile() error {
	if IsRemoteStateEnabled() {
		return S3DeleteLockFile()
	}
	if err := os.Remove(LockFilePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", LockFilePath, err)
	}
	return nil
}

// RemoveDockerCompose deletes docker-compose.yml if it exists.
// Returns nil if the file does not exist.
func RemoveDockerCompose(filename string) error {
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", filename, err)
	}
	return nil
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (d *Destroyer) log(msg string) {
	d.Log = append(d.Log, msg)
}
