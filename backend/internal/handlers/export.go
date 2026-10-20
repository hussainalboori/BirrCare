package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"birrcare/backend/internal/models"
)

type ExportHandler struct {
	DB *sql.DB
}

type DoctorSummaryResponse struct {
	GeneratedAt time.Time          `json:"generated_at"`
	DaysCovered int                `json:"days_covered"`
	StartDate   string             `json:"start_date"`
	EndDate     string             `json:"end_date"`
	VitalsStats VitalsStats        `json:"vitals_stats"`
	Adherence   MedAdherence       `json:"adherence"`
	Alertness   map[string]int     `json:"alertness_distribution"`
	RecentNotes []models.HandoverNote `json:"recent_notes"`
	RecentVitals []models.VitalsLog `json:"recent_vitals"`
	// Every injection with the reading and what the scale said, so the doctor can
	// see how the scale is performing and adjust it.
	InsulinLogs []InsulinLog `json:"insulin_logs"`
}

type VitalsStats struct {
	TotalReadings   int     `json:"total_readings"`
	AvgSystolic     float64 `json:"avg_systolic"`
	MaxSystolic     int     `json:"max_systolic"`
	MinSystolic     int     `json:"min_systolic"`
	AvgDiastolic    float64 `json:"avg_diastolic"`
	MaxDiastolic    int     `json:"max_diastolic"`
	MinDiastolic    int     `json:"min_diastolic"`
	AvgPulse        float64 `json:"avg_pulse"`
	CrisisCount     int     `json:"crisis_count"`
	ElevatedCount   int     `json:"elevated_count"`
	AvgBloodSugar   *float64 `json:"avg_blood_sugar,omitempty"`
}

type MedAdherence struct {
	TotalScheduled int     `json:"total_scheduled"`
	TotalTaken     int     `json:"total_taken"`
	TotalSkipped   int     `json:"total_skipped"`
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
		       c.name, c.avatar_color
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
		WHERE v.recorded_at >= ?
		ORDER BY v.recorded_at DESC
	`, startDateStr)

	var stats VitalsStats
	alertnessMap := map[string]int{"alert": 0, "confused": 0, "restless": 0, "drowsy": 0}
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
			if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &sys, &dia, &pulse, &sugar, &v.AlertnessLevel, &vitalType, &notes, &v.CaregiverName, &v.AvatarColor); err != nil {
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
				sumSugar += sugar.Float64
				sugarCount++
			}
			if vitalType.Valid {
				v.VitalType = vitalType.String
			}
			if notes.Valid {
				v.Notes = notes.String
			}
			v.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAtStr)

			stats.TotalReadings++
			alertnessMap[v.AlertnessLevel]++
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

	// 2. Medication adherence
	var totalTaken, totalSkipped int
	_ = h.DB.QueryRow(`
		SELECT COUNT(CASE WHEN status = 'taken' THEN 1 END),
		       COUNT(CASE WHEN status = 'skipped' THEN 1 END)
		FROM medication_logs
		WHERE scheduled_date >= ? AND voided_at IS NULL -- a voided mis-tap was never given
	`, startDateStr).Scan(&totalTaken, &totalSkipped)

	totalScheduled := totalTaken + totalSkipped
	var rate float64
	if totalScheduled > 0 {
		rate = (float64(totalTaken) / float64(totalScheduled)) * 100.0
	}

	adherence := MedAdherence{
		TotalScheduled: totalScheduled,
		TotalTaken:     totalTaken,
		TotalSkipped:   totalSkipped,
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
		GeneratedAt:  time.Now().UTC(),
		DaysCovered:  days,
		StartDate:    startDateStr,
		EndDate:      endDateStr,
		VitalsStats:  stats,
		Adherence:    adherence,
		Alertness:    alertnessMap,
		RecentNotes:  recentNotes,
		RecentVitals: recentVitals,
		InsulinLogs:  insulinLogs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
