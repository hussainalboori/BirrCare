package handlers

import (
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
