package handlers

import "time"

// IntervalHoursForTest exposes intervalHours to the external test package.
var IntervalHoursForTest = intervalHours

// SetClinicalLocationForTest overrides the household time zone (APP_TIMEZONE).
func SetClinicalLocationForTest(loc *time.Location) {
	clinicalLocOnce.Do(func() {})
	clinicalLoc = loc
}
