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

	endDate := time.Now().UTC()
	startDate := endDate.AddDate(0, 0, -days)
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 1. Vitals stats
	rows, err := h.DB.Query(`
		SELECT v.id, v.caregiver_id, v.recorded_at, v.systolic, v.diastolic, v.pulse, v.blood_sugar, v.alertness_level, v.notes,
		       c.name, c.avatar_color
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
		WHERE v.recorded_at >= ?
		ORDER BY v.recorded_at DESC
	`, startDateStr)

	var stats VitalsStats
	alertnessMap := map[string]int{"alert": 0, "confused": 0, "restless": 0, "drowsy": 0}
	var recentVitals []models.VitalsLog
	var sumSys, sumDia, sumPulse, sumSugar float64
	var sugarCount int

	if err == nil {
		defer rows.Close()
		first := true
		for rows.Next() {
			var v models.VitalsLog
			var notes sql.NullString
			var sugar sql.NullFloat64
			var recordedAtStr string
			if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &v.Systolic, &v.Diastolic, &v.Pulse, &sugar, &v.AlertnessLevel, &notes, &v.CaregiverName, &v.AvatarColor); err != nil {
				continue
			}
			if notes.Valid {
				v.Notes = notes.String
			}
			if sugar.Valid {
				v.BloodSugar = &sugar.Float64
				sumSugar += sugar.Float64
				sugarCount++
			}
			v.RecordedAt, _ = time.Parse("2006-01-02 15:04:05", recordedAtStr)
			v.BPStatus = CalculateBPStatus(v.Systolic, v.Diastolic)

			stats.TotalReadings++
			sumSys += float64(v.Systolic)
			sumDia += float64(v.Diastolic)
			sumPulse += float64(v.Pulse)

			if first {
				stats.MinSystolic = v.Systolic
				stats.MaxSystolic = v.Systolic
				stats.MinDiastolic = v.Diastolic
				stats.MaxDiastolic = v.Diastolic
				first = false
			} else {
				if v.Systolic < stats.MinSystolic {
					stats.MinSystolic = v.Systolic
				}
				if v.Systolic > stats.MaxSystolic {
					stats.MaxSystolic = v.Systolic
				}
				if v.Diastolic < stats.MinDiastolic {
					stats.MinDiastolic = v.Diastolic
				}
				if v.Diastolic > stats.MaxDiastolic {
					stats.MaxDiastolic = v.Diastolic
				}
			}

			if v.BPStatus == "CRISIS" {
				stats.CrisisCount++
			} else if v.BPStatus == "STAGE_2" || v.BPStatus == "ELEVATED" {
				stats.ElevatedCount++
			}

			alertnessMap[v.AlertnessLevel]++
			recentVitals = append(recentVitals, v)
		}
	}

	if stats.TotalReadings > 0 {
		stats.AvgSystolic = sumSys / float64(stats.TotalReadings)
		stats.AvgDiastolic = sumDia / float64(stats.TotalReadings)
		stats.AvgPulse = sumPulse / float64(stats.TotalReadings)
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
		WHERE scheduled_date >= ?
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

	var recentNotes []models.HandoverNote
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
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
