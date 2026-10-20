package handlers_test

import (
	"encoding/json"
	"os"
	"testing"

	"birrcare/backend/internal/handlers"
)

// Issue #13: the Go and TypeScript classifiers are tested against the same cases.
func TestBPStatusSharedFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../clinical/bp-cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		LowSystolic  int `json:"low_systolic"`
		LowDiastolic int `json:"low_diastolic"`
		Cases        []struct {
			Sys, Dia int
			Expected string
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: f.LowSystolic, LowDiastolic: f.LowDiastolic})
	for _, c := range f.Cases {
		if got := handlers.CalculateBPStatus(c.Sys, c.Dia); got != c.Expected {
			t.Errorf("%d/%d: want %s, got %s", c.Sys, c.Dia, c.Expected, got)
		}
	}
	// The doctor's own thresholds move the LOW band.
	handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: 100, LowDiastolic: 60})
	defer handlers.SetBPLowThresholds(handlers.BPLowThresholds{LowSystolic: 90, LowDiastolic: 60})
	if got := handlers.CalculateBPStatus(95, 70); got != "LOW" {
		t.Errorf("95/70 with a 100 systolic floor: want LOW, got %s", got)
	}
}
