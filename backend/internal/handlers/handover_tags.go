package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"birrcare/backend/internal/models"
)

// Issue #18: fixed quick-tap observations for a handover, so a busy caregiver can
// hand over in a few taps instead of typing. The frontend shows the same list in
// Arabic and English (frontend/src/lib/handoverTags.ts).
var handoverTags = map[string]bool{
	"ate_well": false, "ate_half": false, "low_appetite": false,
	"swallowing_difficulty": true, // urgent: aspiration risk after a stroke
	"needs_rest":            false, "good_mood": false, "agitated": false,
	"pain":       true, // urgent
	"slept_well": false, "poor_sleep": false, "bowel_movement": false, "repositioned": false,
}

// validateTags de-duplicates tags, rejects unknown ones, and reports whether any
// of them makes the note urgent.
func validateTags(tags []string) ([]string, bool, error) {
	seen := map[string]bool{}
	out := []string{}
	urgent := false
	for _, t := range tags {
		u, ok := handoverTags[t]
		if !ok {
			return nil, false, fmt.Errorf("unknown tag %q", t)
		}
		if seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		urgent = urgent || u
	}
	return out, urgent, nil
}

func tagsJSON(tags []string) interface{} {
	if len(tags) == 0 {
		return nil
	}
	b, _ := json.Marshal(tags)
	return string(b)
}

// handoverNoteSelect reads notes with their tags and any duty they passed.
const handoverNoteSelect = `
	SELECT n.id, n.caregiver_id, n.created_at, n.shift_type, n.summary, n.urgent_flag,
	       c.name, c.avatar_color, COALESCE(n.tags, ''),
	       COALESCE(d.id, ''), COALESCE(d.to_caregiver_id, ''), COALESCE(t.name, ''), COALESCE(d.accepted_at, '')
	FROM handover_notes n
	JOIN caregivers c ON n.caregiver_id = c.id
	LEFT JOIN duty_handovers d ON d.note_id = n.id
	LEFT JOIN caregivers t ON t.id = d.to_caregiver_id`

func scanHandoverNote(rows *sql.Rows) (models.HandoverNote, error) {
	var n models.HandoverNote
	var shiftType sql.NullString
	var createdAtStr, tags, acceptedAt string
	var urgentInt int
	if err := rows.Scan(&n.ID, &n.CaregiverID, &createdAtStr, &shiftType, &n.Summary, &urgentInt, &n.CaregiverName, &n.AvatarColor,
		&tags, &n.DutyHandoverID, &n.PassedToID, &n.PassedToName, &acceptedAt); err != nil {
		return n, err
	}
	if shiftType.Valid {
		n.ShiftType = shiftType.String
	}
	n.UrgentFlag = urgentInt == 1
	n.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
	if tags != "" {
		_ = json.Unmarshal([]byte(tags), &n.Tags)
	}
	if acceptedAt != "" {
		n.AcceptedAt = sqliteToRFC3339(acceptedAt)
	}
	return n, nil
}
