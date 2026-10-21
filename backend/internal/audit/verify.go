package audit

import (
	"fmt"
)

// VerifyResult reports whether the tamper-evident audit hash chain is intact,
// and indicates the first broken link if tampering is detected.
type VerifyResult struct {
	Valid        bool   `json:"valid"`
	BrokenID     string `json:"broken_id,omitempty"`
	BrokenRowID  string `json:"broken_row_id,omitempty"`
	ID           string `json:"id,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Message      string `json:"message,omitempty"`
	VerifiedRows int    `json:"verified_rows"`
	Count        int    `json:"count"`
}

// Verify walks the audit_logs hash chain from earliest to latest row and verifies
// each link's integrity. If any row was tampered with, it returns a result pointing
// to that row's ID.
func Verify(database DBTX) (*VerifyResult, error) {
	rows, err := database.Query(`
		SELECT id, COALESCE(caregiver_id, ''), action, entity_type, COALESCE(entity_id, ''), COALESCE(details, ''), created_at, COALESCE(prev_hash, ''), COALESCE(row_hash, '')
		FROM audit_logs
		ORDER BY rowid ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed querying audit logs: %w", err)
	}
	defer rows.Close()

	var prevHash string
	verified := 0

	for rows.Next() {
		var id, caregiverID, action, entityType, entityID, details, createdAt, rowPrevHash, rowHash string
		if err := rows.Scan(&id, &caregiverID, &action, &entityType, &entityID, &details, &createdAt, &rowPrevHash, &rowHash); err != nil {
			return nil, fmt.Errorf("failed scanning audit row: %w", err)
		}

		if rowPrevHash != prevHash {
			return &VerifyResult{
				Valid:        false,
				BrokenID:     id,
				BrokenRowID:  id,
				ID:           id,
				Reason:       fmt.Sprintf("prev_hash mismatch: expected %q, got %q", prevHash, rowPrevHash),
				Message:      fmt.Sprintf("broken link at row %s", id),
				VerifiedRows: verified,
				Count:        verified,
			}, nil
		}

		expectedHash := ComputeHash(rowPrevHash, id, caregiverID, action, entityID, details, createdAt)
		if rowHash != expectedHash {
			return &VerifyResult{
				Valid:        false,
				BrokenID:     id,
				BrokenRowID:  id,
				ID:           id,
				Reason:       fmt.Sprintf("row_hash mismatch: expected %q, got %q", expectedHash, rowHash),
				Message:      fmt.Sprintf("tampered details or metadata at row %s", id),
				VerifiedRows: verified,
				Count:        verified,
			}, nil
		}

		prevHash = rowHash
		verified++
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading audit rows: %w", err)
	}

	return &VerifyResult{
		Valid:        true,
		VerifiedRows: verified,
		Count:        verified,
	}, nil
}
