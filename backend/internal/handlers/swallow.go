package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Crush, split and food guidance per medicine (issue #64). After a stroke many
// patients have trouble swallowing and families crush pills into yogurt; some
// tablets (slow-release, coated) must never be crushed. The admin fills these in
// from the box or the pharmacist; until then a medicine says "ask the pharmacist".

const (
	crushOK            = "ok"
	crushDoNot         = "do_not_crush"
	crushAskPharmacist = "ask_pharmacist"
	swallowWindow      = 7 * 24 * time.Hour
)

var (
	validCrush    = map[string]bool{crushOK: true, crushDoNot: true, crushAskPharmacist: true}
	validWithFood = map[string]bool{"with_food": true, "empty_stomach": true, "any": true}
)

// SwallowFields are shared by create and update. split_ok is true, false, or
// null ("not known"); on an update an absent split_ok keeps the old value.
type SwallowFields struct {
	Crush    *string         `json:"crush,omitempty"`
	SplitOK  json.RawMessage `json:"split_ok,omitempty"`
	WithFood *string         `json:"with_food,omitempty"`
}

// validate returns an error code and message, or "" when the fields are fine.
func (f *SwallowFields) validate() (string, string) {
	if f.Crush != nil && !validCrush[*f.Crush] {
		return "INVALID_CRUSH", "crush must be ok, do_not_crush or ask_pharmacist"
	}
	if f.WithFood != nil && !validWithFood[*f.WithFood] {
		return "INVALID_WITH_FOOD", "with_food must be with_food, empty_stomach or any"
	}
	if _, _, err := f.splitValue(); err != nil {
		return "INVALID_SPLIT_OK", "split_ok must be true, false or null"
	}
	return "", ""
}

// splitValue: set = the field was sent; v = the value to store (nil = not known).
func (f *SwallowFields) splitValue() (set bool, v interface{}, err error) {
	if len(f.SplitOK) == 0 {
		return false, nil, nil
	}
	if bytes.Equal(bytes.TrimSpace(f.SplitOK), []byte("null")) {
		return true, nil, nil
	}
	var b bool
	if err := json.Unmarshal(f.SplitOK, &b); err != nil {
		return false, nil, err
	}
	return true, b, nil
}

// insertValues are the crush, split_ok and with_food columns for a new medicine.
func (f *SwallowFields) insertValues() (string, interface{}, string) {
	crush, withFood := crushAskPharmacist, "any"
	if f.Crush != nil {
		crush = *f.Crush
	}
	if f.WithFood != nil {
		withFood = *f.WithFood
	}
	_, split, _ := f.splitValue()
	return crush, split, withFood
}

// apply saves the fields that were sent, in the edit's transaction.
func (f *SwallowFields) apply(tx *sql.Tx, medID string) error {
	if f.Crush != nil {
		if _, err := tx.Exec(`UPDATE medications SET crush = ? WHERE id = ?`, *f.Crush, medID); err != nil {
			return err
		}
	}
	if f.WithFood != nil {
		if _, err := tx.Exec(`UPDATE medications SET with_food = ? WHERE id = ?`, *f.WithFood, medID); err != nil {
			return err
		}
	}
	if set, v, _ := f.splitValue(); set {
		if _, err := tx.Exec(`UPDATE medications SET split_ok = ? WHERE id = ?`, v, medID); err != nil {
			return err
		}
	}
	return nil
}

func nullableBool(v sql.NullBool) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Bool
	return &b
}

// SwallowNote is the latest "swallowing difficulty" handover tag of the last 7 days.
type SwallowNote struct {
	At string `json:"at"` // RFC3339
	By string `json:"by,omitempty"`
}

// recentSwallowingDifficulty returns the latest swallowing_difficulty tag logged
// in the 7 days before now, or nil.
func recentSwallowingDifficulty(q querier, now time.Time) (*SwallowNote, error) {
	var at, by string
	err := q.QueryRow(`SELECT n.created_at, COALESCE(c.name, '') FROM handover_notes n LEFT JOIN caregivers c ON c.id = n.caregiver_id
		WHERE n.tags LIKE '%"swallowing_difficulty"%' AND datetime(n.created_at) >= datetime(?) AND datetime(n.created_at) <= datetime(?)
		ORDER BY datetime(n.created_at) DESC LIMIT 1`,
		now.Add(-swallowWindow).UTC().Format(sqliteTimeLayout), now.UTC().Format(sqliteTimeLayout)).Scan(&at, &by)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	return &SwallowNote{At: sqliteToRFC3339(at), By: by}, nil
}
