package models

import "strings"

// EmergencyAlertKinds are the alerts that mean "something may be happening to her
// right now" (issue #54). It is the one list behind the backup and update guards,
// push urgency and the red crisis bar (the "emergency" flag on /alerts/unacked).
// clinical/alert-kinds.json mirrors it for the Go and TypeScript tests.
var EmergencyAlertKinds = []string{"bp_crisis", "bp_low", "fast_positive", "spo2_low", "sugar_low", "alertness", "fever_high"}

// IsEmergencyAlert reports whether an alert kind is an emergency.
func IsEmergencyAlert(kind string) bool {
	for _, k := range EmergencyAlertKinds {
		if k == kind {
			return true
		}
	}
	return false
}

// EmergencyAlertKindsSQL is the list as an SQL IN (...) body, built from the
// constants above (never from input).
func EmergencyAlertKindsSQL() string {
	return "'" + strings.Join(EmergencyAlertKinds, "', '") + "'"
}
