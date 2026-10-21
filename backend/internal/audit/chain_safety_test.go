package audit_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/db"
)

// Concurrent best-effort audit.Log calls (outside any transaction) must still
// produce one unbroken chain: /audit/verify must not cry tampering under load.
func TestConcurrentAuditLogKeepsChainIntact(t *testing.T) {
	database, err := db.InitDB(filepath.Join(t.TempDir(), "birrcare.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			audit.Log(database, "", "CONCURRENT", "test", "x", "parallel write")
		}()
	}
	wg.Wait()
	res, err := audit.Verify(database)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Valid {
		t.Fatalf("40 concurrent writes broke the chain: %+v", res)
	}
}

// The operator's 2FA recovery (DISABLE_2FA_FOR) writes an audit row during boot.
// The backend must still start on every later boot, and the chain must stay valid.
func TestBootAfterOperatorTwoFactorResetStillStarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "birrcare.db")
	database, err := db.InitDB(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	var id string
	if err := database.QueryRow(`SELECT id FROM caregivers LIMIT 1`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`UPDATE caregivers SET totp_enabled = 1 WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	database.Close()

	os.Setenv("DISABLE_2FA_FOR", id)
	defer os.Unsetenv("DISABLE_2FA_FOR")
	for boot := 2; boot <= 3; boot++ {
		database, err = db.InitDB(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Migrate(database); err != nil {
			t.Fatalf("boot %d after a 2FA reset failed: %v", boot, err)
		}
		res, err := audit.Verify(database)
		if err != nil || !res.Valid {
			t.Fatalf("boot %d: chain not valid after a 2FA reset: %+v %v", boot, res, err)
		}
		database.Close()
	}
}
