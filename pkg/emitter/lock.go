// lock.go — Sajon State Management (sajon.lock)
//
// Provides idempotent cloud provisioning by persisting cloud resource metadata
// to a JSON lock file (sajon.lock) in the current working directory OR an
// AWS S3 bucket when SAJON_REMOTE_BUCKET is configured.
//
// State routing:
//   - Remote mode (SAJON_REMOTE_BUCKET set with AWS creds): all reads/writes
//     go to S3.  Local disk is never touched in remote mode.
//   - Local mode (default): reads/writes go to sajon.lock on disk.
//
// On every `sajon up` run the emitter:
//  1. Reads sajon.lock (disk or S3).
//  2. For each Neon-eligible resource, checks whether an entry with
//     status == "active" already exists in the lock.
//  3. If yes  → skips the Neon API call and reuses the cached metadata.
//  4. If no   → calls the Neon API, then writes / updates the lock file.
//
// The lock file is human-readable JSON so engineers can inspect or manually
// correct it. Corrupted / missing files are treated as a fresh deployment.

package emitter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// ── Lock file path constant ────────────────────────────────────────────────────

const LockFilePath = "sajon.lock"

// ── Data types ────────────────────────────────────────────────────────────────

// LockResource holds the persisted metadata for a single cloud resource.
type LockResource struct {
	Provider         string `json:"provider"`
	Type             string `json:"type"`
	ProjectID        string `json:"project_id"`
	ConnectionString string `json:"connection_string"`
	Host             string `json:"host,omitempty"`
	PoolerHost       string `json:"pooler_host,omitempty"`
	Database         string `json:"database,omitempty"`
	User             string `json:"user,omitempty"`
	Region           string `json:"region,omitempty"`   // cloud region, e.g. "ap-south-1"
	Password         string `json:"password,omitempty"` // master password (AWS RDS only — needed to rebuild DSN from cache)
	Port             int    `json:"port,omitempty"`      // DB port (AWS RDS only — default 5432)
	InstanceID       string `json:"instance_id,omitempty"` // AWS RDS instance identifier
	Status           string `json:"status"` // "active" | "failed"
}

// LockFile is the top-level structure persisted to sajon.lock.
type LockFile struct {
	Resources map[string]LockResource `json:"resources"`
}

// ── Read ──────────────────────────────────────────────────────────────────────

// ReadLockFile loads sajon.lock from the configured backend (S3 or local disk).
// Returns an empty LockFile (not an error) when the file/object is missing or
// contains invalid JSON — both cases are treated as "start fresh".
func ReadLockFile() (*LockFile, error) {
	// ── Remote (S3) path ──────────────────────────────────────────────────
	if IsRemoteStateEnabled() {
		return S3DownloadLockFile()
	}

	// ── Local disk path ───────────────────────────────────────────────────
	data, err := os.ReadFile(LockFilePath)
	if os.IsNotExist(err) {
		// File doesn't exist yet — return a blank lock.
		return &LockFile{Resources: make(map[string]LockResource)}, nil
	}
	if err != nil {
		// Unexpected I/O error (permissions, etc.) — surface it.
		return nil, fmt.Errorf("read %s: %w", LockFilePath, err)
	}

	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var lf LockFile
	if jsonErr := json.Unmarshal(data, &lf); jsonErr != nil {
		// Corrupted JSON — warn and treat as fresh deployment.
		fmt.Printf("  [⚠️ ] %s is corrupted (invalid JSON) — treating as fresh deployment.\n", LockFilePath)
		return &LockFile{Resources: make(map[string]LockResource)}, nil
	}

	// Ensure the map is initialised even for well-formed files that omit "resources".
	if lf.Resources == nil {
		lf.Resources = make(map[string]LockResource)
	}
	return &lf, nil
}

// ── Lookup ────────────────────────────────────────────────────────────────────

// GetActiveResource returns the LockResource for resourceName if (and only if)
// its status is "active". The second return value is false when the resource
// is absent or its status is anything other than "active".
func (lf *LockFile) GetActiveResource(resourceName string) (LockResource, bool) {
	if lf == nil || lf.Resources == nil {
		return LockResource{}, false
	}
	lr, ok := lf.Resources[resourceName]
	if !ok || lr.Status != "active" {
		return LockResource{}, false
	}
	return lr, true
}

// ── Write ─────────────────────────────────────────────────────────────────────

// UpsertResource inserts or replaces the entry for resourceName in the lock
// and immediately flushes the entire file to disk as indented JSON.
func (lf *LockFile) UpsertResource(resourceName string, lr LockResource) error {
	if lf.Resources == nil {
		lf.Resources = make(map[string]LockResource)
	}
	lf.Resources[resourceName] = lr
	return lf.Flush()
}

// Flush writes the current in-memory LockFile state to the configured backend
// (S3 when remote state is enabled, local disk otherwise).
func (lf *LockFile) Flush() error {
	// ── Remote (S3) path ──────────────────────────────────────────────────
	if IsRemoteStateEnabled() {
		return S3UploadLockFile(lf)
	}

	// ── Local disk path ───────────────────────────────────────────────────
	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", LockFilePath, err)
	}
	if err := os.WriteFile(LockFilePath, data, 0644); err != nil {
		return fmt.Errorf("write %s: %w", LockFilePath, err)
	}
	return nil
}
