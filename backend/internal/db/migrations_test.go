package db

import (
	"database/sql"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

func TestSeedSuperAdminFromEnv(t *testing.T) {
	t.Setenv("SUPER_ADMIN_USER", "chief_admin")
	t.Setenv("SUPER_ADMIN_PIN", "9876")

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed opening in-memory db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("failed migration: %v", err)
	}

	var name, email, role, pinHash string
	err = db.QueryRow("SELECT name, email, role, pin_hash FROM caregivers WHERE id = 'cg_superadmin'").Scan(&name, &email, &role, &pinHash)
	if err != nil {
		t.Fatalf("failed querying seeded super admin: %v", err)
	}

	if name != "chief_admin" {
		t.Errorf("expected name 'chief_admin', got %q", name)
	}
	if email != "chief_admin@birrcare.local" {
		t.Errorf("expected email 'chief_admin@birrcare.local', got %q", email)
	}
	if role != "super_admin" {
		t.Errorf("expected role 'super_admin', got %q", role)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte("9876")); err != nil {
		t.Errorf("bcrypt verification failed for custom PIN: %v", err)
	}
}

func TestSeedSuperAdminDefaults(t *testing.T) {
	t.Setenv("SUPER_ADMIN_USER", "")
	t.Setenv("SUPER_ADMIN_PIN", "")

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed opening in-memory db: %v", err)
	}
	defer db.Close()

	if err := Migrate(db); err != nil {
		t.Fatalf("failed migration: %v", err)
	}

	var name, email, role, pinHash string
	err = db.QueryRow("SELECT name, email, role, pin_hash FROM caregivers WHERE id = 'cg_superadmin'").Scan(&name, &email, &role, &pinHash)
	if err != nil {
		t.Fatalf("failed querying seeded super admin: %v", err)
	}

	if name != "admin" {
		t.Errorf("expected default name 'admin', got %q", name)
	}
	if email != "admin@birrcare.local" {
		t.Errorf("expected default email 'admin@birrcare.local', got %q", email)
	}
	if role != "super_admin" {
		t.Errorf("expected role 'super_admin', got %q", role)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(pinHash), []byte("1234")); err != nil {
		t.Errorf("bcrypt verification failed for default PIN: %v", err)
	}

	// Verify no dummy medications were seeded
	var medCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM medications").Scan(&medCount); err != nil {
		t.Fatalf("failed querying medications count: %v", err)
	}
	if medCount != 0 {
		t.Errorf("expected 0 seeded medications, got %d", medCount)
	}
}
