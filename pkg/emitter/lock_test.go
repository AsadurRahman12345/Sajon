// lock_test.go — Unit tests for sajon.lock state management
//
// Tests all three scenarios:
//  Test 1: Fresh deploy — no lock file exists, lock is created after provision
//  Test 2: Cached hit  — lock file exists with "active" status, API is skipped
//  Test 3: Corrupted   — invalid JSON in lock file, treated as fresh deploy
//  Test 4: Missing key — lock exists but resource not in it, treated as fresh
//  Test 5: Status != active — entry exists but status is "failed", re-provisions
package emitter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// tempLockPath overrides the lock file to a temp file for each test.
// It returns the path and a cleanup function.
func tempLockDir(t *testing.T) (string, func()) {
	t.Helper()
	dir := t.TempDir()
	// Override the package-level constant via test working directory trick:
	// We'll change to the temp dir so ReadLockFile() finds the file there.
	original, _ := os.Getwd()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir to temp dir: %v", err)
	}
	return dir, func() {
		_ = os.Chdir(original)
	}
}

func writeLockJSON(t *testing.T, dir string, content string) {
	t.Helper()
	path := filepath.Join(dir, LockFilePath)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write lock file: %v", err)
	}
}

// ── Test 1: No lock file → ReadLockFile returns empty LockFile ────────────────

func TestReadLockFile_NoFile(t *testing.T) {
	_, cleanup := tempLockDir(t)
	defer cleanup()

	lf, err := ReadLockFile()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if lf == nil {
		t.Fatal("expected non-nil LockFile")
	}
	if len(lf.Resources) != 0 {
		t.Fatalf("expected empty resources map, got %d entries", len(lf.Resources))
	}
	t.Log("✅  Test 1 PASS: No lock file → returns empty LockFile (fresh deploy)")
}

// ── Test 2: Valid lock with active resource → GetActiveResource returns it ────

func TestGetActiveResource_Hit(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	payload := `{
  "resources": {
    "rentic_prod": {
      "provider": "neon",
      "type": "postgres",
      "project_id": "proj_abc123",
      "connection_string": "postgres://user:pass@ep-cool.aws.neon.tech/neondb?sslmode=require",
      "host": "ep-cool.aws.neon.tech",
      "pooler_host": "ep-cool-pooler.aws.neon.tech",
      "database": "neondb",
      "user": "user",
      "status": "active"
    }
  }
}`
	writeLockJSON(t, dir, payload)

	lf, err := ReadLockFile()
	if err != nil {
		t.Fatalf("ReadLockFile error: %v", err)
	}

	lr, found := lf.GetActiveResource("rentic_prod")
	if !found {
		t.Fatal("expected to find active resource 'rentic_prod', but got miss")
	}
	if lr.ProjectID != "proj_abc123" {
		t.Fatalf("expected project_id 'proj_abc123', got '%s'", lr.ProjectID)
	}
	if lr.Status != "active" {
		t.Fatalf("expected status 'active', got '%s'", lr.Status)
	}
	t.Logf("✅  Test 2 PASS: Active resource found → ProjectID=%s, Status=%s", lr.ProjectID, lr.Status)
}

// ── Test 3: Corrupted JSON → treated as fresh deploy ─────────────────────────

func TestReadLockFile_CorruptedJSON(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	writeLockJSON(t, dir, `{ this is NOT valid JSON !!! }`)

	lf, err := ReadLockFile()
	if err != nil {
		t.Fatalf("expected no error on corrupted file (should fallback), got: %v", err)
	}
	if len(lf.Resources) != 0 {
		t.Fatalf("expected empty resources after corruption fallback, got %d", len(lf.Resources))
	}
	t.Log("✅  Test 3 PASS: Corrupted JSON → returns empty LockFile (fresh deploy fallback)")
}

// ── Test 4: Resource name not in lock → GetActiveResource returns false ───────

func TestGetActiveResource_Miss_NotInLock(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	payload := `{
  "resources": {
    "other_db": {
      "provider": "neon",
      "type": "postgres",
      "project_id": "proj_xyz",
      "connection_string": "postgres://...",
      "status": "active"
    }
  }
}`
	writeLockJSON(t, dir, payload)

	lf, err := ReadLockFile()
	if err != nil {
		t.Fatalf("ReadLockFile error: %v", err)
	}

	_, found := lf.GetActiveResource("rentic_prod")
	if found {
		t.Fatal("expected miss for 'rentic_prod' (not in lock), but got hit")
	}
	t.Log("✅  Test 4 PASS: Resource not in lock → returns miss (will re-provision)")
}

// ── Test 5: Resource in lock but status != "active" → returns miss ────────────

func TestGetActiveResource_Miss_FailedStatus(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	payload := `{
  "resources": {
    "rentic_prod": {
      "provider": "neon",
      "type": "postgres",
      "project_id": "proj_abc123",
      "connection_string": "postgres://...",
      "status": "failed"
    }
  }
}`
	writeLockJSON(t, dir, payload)

	lf, err := ReadLockFile()
	if err != nil {
		t.Fatalf("ReadLockFile error: %v", err)
	}

	_, found := lf.GetActiveResource("rentic_prod")
	if found {
		t.Fatal("expected miss for 'rentic_prod' with status='failed', but got hit")
	}
	t.Log("✅  Test 5 PASS: status='failed' → returns miss (will re-provision)")
}

// ── Test 6: UpsertResource writes correct JSON to disk ────────────────────────

func TestUpsertResource_WritesToDisk(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	lf := &LockFile{Resources: make(map[string]LockResource)}
	err := lf.UpsertResource("rentic_prod", LockResource{
		Provider:         "neon",
		Type:             "postgres",
		ProjectID:        "proj_test999",
		ConnectionString: "postgres://sajon:pass@ep-test.aws.neon.tech/neondb?sslmode=require",
		Host:             "ep-test.aws.neon.tech",
		Database:         "neondb",
		User:             "sajon",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("UpsertResource error: %v", err)
	}

	// Read the written file back
	raw, err := os.ReadFile(filepath.Join(dir, LockFilePath))
	if err != nil {
		t.Fatalf("could not read lock file after upsert: %v", err)
	}

	var parsed LockFile
	if jsonErr := json.Unmarshal(raw, &parsed); jsonErr != nil {
		t.Fatalf("lock file written by UpsertResource is not valid JSON: %v", jsonErr)
	}

	lr, ok := parsed.Resources["rentic_prod"]
	if !ok {
		t.Fatal("'rentic_prod' not found in the written lock file")
	}
	if lr.ProjectID != "proj_test999" {
		t.Fatalf("expected project_id 'proj_test999', got '%s'", lr.ProjectID)
	}
	if lr.Status != "active" {
		t.Fatalf("expected status 'active', got '%s'", lr.Status)
	}
	t.Logf("✅  Test 6 PASS: UpsertResource wrote valid JSON → ProjectID=%s, Status=%s", lr.ProjectID, lr.Status)
	t.Logf("     Lock file contents:\n%s", string(raw))
}

// ── Test 7: Multiple resources in one lock file ───────────────────────────────

func TestUpsertResource_MultipleResources(t *testing.T) {
	_, cleanup := tempLockDir(t)
	defer cleanup()

	lf := &LockFile{Resources: make(map[string]LockResource)}

	resources := []struct {
		name string
		lr   LockResource
	}{
		{"db_one", LockResource{Provider: "neon", Type: "postgres", ProjectID: "proj_001", ConnectionString: "postgres://...", Status: "active"}},
		{"db_two", LockResource{Provider: "neon", Type: "postgres", ProjectID: "proj_002", ConnectionString: "postgres://...", Status: "active"}},
		{"db_three", LockResource{Provider: "neon", Type: "postgres", ProjectID: "proj_003", ConnectionString: "postgres://...", Status: "active"}},
	}

	for _, r := range resources {
		if err := lf.UpsertResource(r.name, r.lr); err != nil {
			t.Fatalf("UpsertResource(%s) error: %v", r.name, err)
		}
	}

	// Verify all 3 can be retrieved
	for _, r := range resources {
		lr, found := lf.GetActiveResource(r.name)
		if !found {
			t.Fatalf("expected to find '%s' in lock, but missed", r.name)
		}
		if lr.ProjectID != r.lr.ProjectID {
			t.Fatalf("resource '%s': expected project_id '%s', got '%s'", r.name, r.lr.ProjectID, lr.ProjectID)
		}
	}
	t.Logf("✅  Test 7 PASS: %d resources written and read back correctly", len(resources))
}

// ── Test 8: Flush overwrites existing file cleanly ───────────────────────────

func TestFlush_Overwrites(t *testing.T) {
	dir, cleanup := tempLockDir(t)
	defer cleanup()

	// Write an initial lock
	writeLockJSON(t, dir, `{"resources":{"old_db":{"provider":"neon","type":"postgres","project_id":"old","connection_string":"","status":"active"}}}`)

	// Load and upsert a completely new resource
	lf, _ := ReadLockFile()
	_ = lf.UpsertResource("new_db", LockResource{
		Provider: "neon", Type: "postgres",
		ProjectID: "proj_new", ConnectionString: "postgres://...", Status: "active",
	})

	// Reload and verify both old_db and new_db exist
	lf2, _ := ReadLockFile()
	if _, ok := lf2.Resources["old_db"]; !ok {
		t.Fatal("old_db was removed after flush — should have been preserved")
	}
	if _, ok := lf2.Resources["new_db"]; !ok {
		t.Fatal("new_db was not written to disk by flush")
	}
	t.Log("✅  Test 8 PASS: Flush preserves existing resources and adds new ones")
}
