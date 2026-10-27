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

// EscalatingAlertKinds are emergencies only when their payload says escalate
// (issue #60): a fall with a head knock, a long or repeated seizure.
var EscalatingAlertKinds = []string{"fall", "seizure"}

// IsEmergency: an emergency kind, or an escalating kind that escalated.
func IsEmergency(kind string, escalate bool) bool {
	if IsEmergencyAlert(kind) {
		return true
	}
	for _, k := range EscalatingAlertKinds {
		if k == kind {
			return escalate
		}
	}
	return false
}

// EmergencyAlertKindsSQL is the list as an SQL IN (...) body, built from the
// constants above (never from input).
func EmergencyAlertKindsSQL() string {
	return "'" + strings.Join(EmergencyAlertKinds, "', '") + "'"
}

// EmergencyAlertWhereSQL matches emergency alerts in a query over alerts aliased
// "a": the emergency kinds, and fall / seizure alerts that escalated.
func EmergencyAlertWhereSQL() string {
	return "(a.kind IN (" + EmergencyAlertKindsSQL() + ") OR (a.kind IN ('" + strings.Join(EscalatingAlertKinds, "', '") +
		"') AND json_extract(a.payload, '$.escalate') = 1))"
}
