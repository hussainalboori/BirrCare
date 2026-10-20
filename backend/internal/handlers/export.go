package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"birrcare/backend/internal/models"
)

type ExportHandler struct {
	DB *sql.DB
}

type DoctorSummaryResponse struct {
	GeneratedAt time.Time    `json:"generated_at"`
	DaysCovered int          `json:"days_covered"`
	StartDate   string       `json:"start_date"`
	EndDate     string       `json:"end_date"`
	VitalsStats VitalsStats  `json:"vitals_stats"`
	Adherence   MedAdherence `json:"adherence"`
	// Per medication: how many doses were given fully, partly, vomited, refused, skipped.
	MedicationCounts []MedDoseCounts       `json:"medication_counts"`
	Alertness        map[string]int        `json:"alertness_distribution"`
	RecentNotes      []models.HandoverNote `json:"recent_notes"`
	RecentVitals     []models.VitalsLog    `json:"recent_vitals"`
	// Every injection with the reading and what the scale said, so the doctor can
	// see how the scale is performing and adjust it.
	InsulinLogs []InsulinLog `json:"insulin_logs"`
}

type VitalsStats struct {
	TotalReadings int      `json:"total_readings"`
	AvgSystolic   float64  `json:"avg_systolic"`
	MaxSystolic   int      `json:"max_systolic"`
	MinSystolic   int      `json:"min_systolic"`
	AvgDiastolic  float64  `json:"avg_diastolic"`
	MaxDiastolic  int      `json:"max_diastolic"`
	MinDiastolic  int      `json:"min_diastolic"`
	AvgPulse      float64  `json:"avg_pulse"`
	CrisisCount   int      `json:"crisis_count"`
	ElevatedCount int      `json:"elevated_count"`
	AvgBloodSugar *float64 `json:"avg_blood_sugar,omitempty"`
	LowCount      int      `json:"low_count"` // low blood pressure readings (issue #13)
	// Positive F.A.S.T. stroke checks in the period.
	FastPositiveCount int `json:"fast_positive_count"`
	// Readings flagged as possibly the old form's pre-filled 6.4; left out of the average.
	UnverifiedSugarCount int `json:"unverified_sugar_count"`
}

type MedDoseCounts struct {
	MedicationID string `json:"medication_id"`
	NameAr       string `json:"name_ar"`
	NameEn       string `json:"name_en"`
	Taken        int    `json:"taken"`
	Partial      int    `json:"partial"`
	Vomited      int    `json:"vomited"` // given (taken/partial) doses later vomited
	Refused      int    `json:"refused"`
	Skipped      int    `json:"skipped"`
}

type MedAdherence struct {
	TotalScheduled int     `json:"total_scheduled"`
	TotalTaken     int     `json:"total_taken"`
	TotalSkipped   int     `json:"total_skipped"`
	TotalPartial   int     `json:"total_partial"`
	TotalRefused   int     `json:"total_refused"`
	TotalVomited   int     `json:"total_vomited"`
	AdherenceRate  float64 `json:"adherence_rate"` // e.g., 95.5 (%)
}

func (h *ExportHandler) ExportDoctorSummary(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 7
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
		days = d
	}

	endDate := time.Now().In(ClinicalLocation())
	startDate := endDate.AddDate(0, 0, -days)
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 1. Vitals stats
	rows, err := h.DB.Query(`
		SELECT v.id, v.caregiver_id, v.recorded_at, v.systolic, v.diastolic, v.pulse, v.blood_sugar, v.alertness_level, v.vital_type, v.notes,
		       c.name, c.avatar_color, v.suspect_default_sugar, COALESCE(v.fast_result, ''), COALESCE(v.last_normal_at, '')
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
		WHERE v.recorded_at >= ?
		ORDER BY v.recorded_at DESC
	`, startDateStr)

	var stats VitalsStats
	alertnessMap := map[string]int{"alert": 0, "confused": 0, "restless": 0, "drowsy": 0, "not_assessed": 0}
	recentVitals := []models.VitalsLog{} // never null in JSON: the report maps over it
	var sumSys, sumDia, sumPulse, sumSugar float64
	var bpCount, pulseCount, sugarCount int

	if err == nil {
		defer rows.Close()
		firstBP := true
		for rows.Next() {
			var v models.VitalsLog
			var sys, dia, pulse sql.NullInt64
			var notes, vitalType sql.NullString
			var sugar sql.NullFloat64
			var recordedAtStr string
			var fastResult, lastNormal string
			if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &sys, &dia, &pulse, &sugar, &v.AlertnessLevel, &vitalType, &notes, &v.CaregiverName, &v.AvatarColor, &v.SuspectDefaultSugar, &fastResult, &lastNormal); err != nil {
				continue
			}
			if sys.Valid && dia.Valid {
				s := int(sys.Int64)
				d := int(dia.Int64)
				v.Systolic = &s
				v.Diastolic = &d
				v.BPStatus = CalculateBPStatus(s, d)
				sumSys += float64(s)
				sumDia += float64(d)
				bpCount++

				if firstBP {
					stats.MinSystolic = s
					stats.MaxSystolic = s
					stats.MinDiastolic = d
					stats.MaxDiastolic = d
					firstBP = false
				} else {
					if s < stats.MinSystolic {
						stats.MinSystolic = s
					}
					if s > stats.MaxSystolic {
						stats.MaxSystolic = s
					}
					if d < stats.MinDiastolic {
						stats.MinDiastolic = d
					}
					if d > stats.MaxDiastolic {
						stats.MaxDiastolic = d
					}
				}

				if v.BPStatus == "CRISIS" {
					stats.CrisisCount++
				} else if v.BPStatus == "LOW" {
					stats.LowCount++
				} else if v.BPStatus == "STAGE_2" || v.BPStatus == "ELEVATED" {
					stats.ElevatedCount++
				}
			}
			if pulse.Valid {
				p := int(pulse.Int64)
				v.Pulse = &p
				sumPulse += float64(p)
				pulseCount++
			}
			if sugar.Valid {
				v.BloodSugar = &sugar.Float64
				// An unverified (possibly pre-filled) 6.4 must not shape the average.
				if v.SuspectDefaultSugar {
					stats.UnverifiedSugarCount++
				} else {
					sumSugar += sugar.Float64
					sugarCount++
				}
			}
			if vitalType.Valid {
				v.VitalType = vitalType.String
			}
			normalizeAlertness(&v)
			if fastResult != "" {
				v.FastResult = json.RawMessage(fastResult)
			}
			v.LastNormalAt = sqliteToRFC3339(lastNormal)
			if v.VitalType == "fast" && (strings.Contains(fastResult, `"face":true`) || strings.Contains(fastResult, `"arm":true`) || strings.Contains(fastResult, `"speech":true`)) {
				stats.FastPositiveCount++
			}
			if notes.Valid {
				v.Notes = notes.String
			}
			v.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAtStr)

			stats.TotalReadings++
			if v.AlertnessLevel == nil {
				alertnessMap["not_assessed"]++
			} else {
				alertnessMap[*v.AlertnessLevel]++
			}
			recentVitals = append(recentVitals, v)
		}
		rows.Close() // release the single pooled connection before the next query
	}

	if bpCount > 0 {
		stats.AvgSystolic = sumSys / float64(bpCount)
		stats.AvgDiastolic = sumDia / float64(bpCount)
	}
	if pulseCount > 0 {
		stats.AvgPulse = sumPulse / float64(pulseCount)
	}
	if sugarCount > 0 {
		avgS := sumSugar / float64(sugarCount)
		stats.AvgBloodSugar = &avgS
	}

	// 2. Medication adherence, per medication. A voided mis-tap was never given; a
	// partial or vomited dose is reported as such, never as fully taken.
	medCounts := []MedDoseCounts{}
	countRows, err := h.DB.Query(`
		SELECT m.id, m.name_ar, m.name_en,
		       COUNT(CASE WHEN l.status = 'taken' THEN 1 END),
		       COUNT(CASE WHEN l.status = 'partial' THEN 1 END),
		       COUNT(CASE WHEN EXISTS (SELECT 1 FROM medication_log_events e WHERE e.log_id = l.id AND e.type = 'vomited') THEN 1 END),
		       COUNT(CASE WHEN l.status = 'refused' THEN 1 END),
		       COUNT(CASE WHEN l.status = 'skipped' THEN 1 END)
		FROM medication_logs l JOIN medications m ON m.id = l.medication_id
		WHERE l.scheduled_date >= ? AND l.voided_at IS NULL
		GROUP BY m.id ORDER BY m.name_en
	`, startDateStr)
	if err == nil {
		for countRows.Next() {
			var c MedDoseCounts
			if err := countRows.Scan(&c.MedicationID, &c.NameAr, &c.NameEn, &c.Taken, &c.Partial, &c.Vomited, &c.Refused, &c.Skipped); err == nil {
				medCounts = append(medCounts, c)
			}
		}
		countRows.Close() // release the single pooled connection before the next query
	}
	var totalTaken, totalSkipped, totalPartial, totalRefused, totalVomited int
	for _, c := range medCounts {
		totalTaken += c.Taken
		totalSkipped += c.Skipped
		totalPartial += c.Partial
		totalRefused += c.Refused
		totalVomited += c.Vomited
	}

	totalScheduled := totalTaken + totalSkipped + totalPartial + totalRefused
	var rate float64
	if totalScheduled > 0 {
		rate = (float64(totalTaken) / float64(totalScheduled)) * 100.0
	}

	adherence := MedAdherence{
		TotalScheduled: totalScheduled,
		TotalTaken:     totalTaken,
		TotalSkipped:   totalSkipped,
		TotalPartial:   totalPartial,
		TotalRefused:   totalRefused,
		TotalVomited:   totalVomited,
		AdherenceRate:  rate,
	}

	// 3. Recent Handover notes
	noteRows, _ := h.DB.Query(`
		SELECT n.id, n.caregiver_id, n.created_at, n.shift_type, n.summary, n.urgent_flag,
		       c.name, c.avatar_color
		FROM handover_notes n
		JOIN caregivers c ON n.caregiver_id = c.id
		WHERE n.created_at >= ?
		ORDER BY n.created_at DESC
		LIMIT 15
	`, startDateStr)

	recentNotes := []models.HandoverNote{}
	if noteRows != nil {
		defer noteRows.Close()
		for noteRows.Next() {
			var n models.HandoverNote
			var shiftType sql.NullString
			var createdAtStr string
			var urgentInt int
			if err := noteRows.Scan(&n.ID, &n.CaregiverID, &createdAtStr, &shiftType, &n.Summary, &urgentInt, &n.CaregiverName, &n.AvatarColor); err == nil {
				if shiftType.Valid {
					n.ShiftType = shiftType.String
				}
				n.UrgentFlag = urgentInt == 1
				n.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAtStr)
				recentNotes = append(recentNotes, n)
			}
		}
	}

	// 4. Insulin (the vitals/notes cursors above are closed, so the connection is free)
	if noteRows != nil {
		noteRows.Close()
	}
	insulinLogs, err := insulinLogsSince(h.DB, startDateStr)
	if err != nil {
		insulinLogs = []InsulinLog{}
	}

	res := DoctorSummaryResponse{
		GeneratedAt:      time.Now().UTC(),
		DaysCovered:      days,
		StartDate:        startDateStr,
		EndDate:          endDateStr,
		VitalsStats:      stats,
		Adherence:        adherence,
		MedicationCounts: medCounts,
		Alertness:        alertnessMap,
		RecentNotes:      recentNotes,
		RecentVitals:     recentVitals,
		InsulinLogs:      insulinLogs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
