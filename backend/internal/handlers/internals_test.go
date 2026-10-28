package handlers

import (
	"database/sql"
	"os"
	"time"
)

// IntervalHoursForTest exposes intervalHours to the external test package.
var IntervalHoursForTest = intervalHours

// SetClinicalLocationForTest overrides the household time zone (APP_TIMEZONE).
func SetClinicalLocationForTest(loc *time.Location) {
	clinicalLocOnce.Do(func() {})
	clinicalLoc = loc
}

// SetWriteAtomicHookForTest replaces how update files are written, so a test
// can fail a write midway. It returns a function that restores the original.
func SetWriteAtomicHookForTest(fn func(f *os.File, data []byte) error) func() {
	orig := writeAtomicHook
	writeAtomicHook = fn
	return func() { writeAtomicHook = orig }
}

// Backup schedule internals (issue #51), for the external tests.
type BackupDueForTest = backupDue

func DecideBackupForTest(now time.Time, loc *time.Location, sch BackupSchedule, st *BackupStatusFile, busy string) backupDue {
	return decideBackup(now, loc, sch, st, busy)
}

func NextBackupSlotForTest(now time.Time, loc *time.Location, times []string) time.Time {
	return nextBackupSlot(now, loc, times)
}

// Emergency card internals (issue #56), for the external tests.
var EmergencyTokenHashForTest = emergencyTokenHash
var TotalDoseForTest = totalDose

// ShiftSummaryForTest runs the night-shift summary (issue #66) at a fixed moment.
func ShiftSummaryForTest(q querier, days int, now time.Time) (map[string]interface{}, error) {
	return shiftSummary(q, days, now)
}

// BuildDailySummaryForTest builds the daily family summary (issue #67).
func BuildDailySummaryForTest(db *sql.DB, date, lang string, now time.Time) (*DailySummary, error) {
	return buildDailySummary(db, date, lang, now)
}
