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
	// Per medication, against the doses the schedule expected (issue #14). Critical
	// medications (anticoagulants, antiplatelets) come first.
	PerMedication []MedReport `json:"per_medication"`
	// Blood pressure per household-local day.
	DailyBP []DailyBP `json:"daily_bp"`
	// Lowest oxygen and highest temperature per household-local day (issue #22).
	DailyOxygenTemp []DailyOxygenTemp `json:"daily_oxygen_temp"`
	// Daily fluid intake per household-local day (issue #28).
	DailyFluid []DailyFluid `json:"daily_fluid"`
	// Bowel movement summary and longest gap between movements (issue #28).
	BowelSummary BowelSummary `json:"bowel_summary"`
	Alertness    map[string]int        `json:"alertness_distribution"`
	RecentNotes  []models.HandoverNote `json:"recent_notes"`
	RecentVitals []models.VitalsLog    `json:"recent_vitals"`
	// Every injection with the reading and what the scale said, so the doctor can
	// see how the scale is performing and adjust it.
	InsulinLogs []InsulinLog `json:"insulin_logs"`
}

// DailyFluid is one household-local day of fluid intake (issue #28).
type DailyFluid struct {
	Date            string `json:"date"`
	TotalML         int    `json:"total_ml"`
	ThickenedML     int    `json:"thickened_ml"`
	TargetML        int    `json:"target_ml"`
	PercentOfTarget int    `json:"percent_of_target"`
	Entries         int    `json:"entries"`
}

// BowelSummary captures frequency, stool consistency, and gaps between bowel movements (issue #28).
type BowelSummary struct {
	TotalMovements int     `json:"total_movements"`
	LastMovementAt *string `json:"last_movement_at,omitempty"`
	DaysSinceLast  *int    `json:"days_since_last,omitempty"`
	LongestGapDays int     `json:"longest_gap_days"`
	HardCount      int     `json:"hard_count"`
	NormalCount    int     `json:"normal_count"`
	LooseCount     int     `json:"loose_count"`
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
	// Issue #22: oxygen and temperature over the period.
	MinSpO2           *int     `json:"min_spo2,omitempty"`
	LowSpO2Count      int      `json:"low_spo2_count"`      // at or below the warning threshold
	CriticalSpO2Count int      `json:"critical_spo2_count"` // at or below the critical threshold
	MaxTemperatureC   *float64 `json:"max_temperature_c,omitempty"`
	FeverCount        int      `json:"fever_count"`
}

// DailyOxygenTemp is one household-local day of SpO2 and temperature readings.
type DailyOxygenTemp struct {
	Date            string   `json:"date"`
	MinSpO2         *int     `json:"min_spo2,omitempty"`
	MaxTemperatureC *float64 `json:"max_temperature_c,omitempty"`
	Readings        int      `json:"readings"`
}

type MedAdherence struct {
	TotalScheduled int `json:"total_scheduled"`
	TotalTaken     int `json:"total_taken"`
	TotalSkipped   int `json:"total_skipped"`
	TotalPartial   int `json:"total_partial"`
	TotalRefused   int `json:"total_refused"`
	TotalVomited   int `json:"total_vomited"`
	// Expected doses with no entry at all: never counted as taken.
	TotalNotRecorded int     `json:"total_not_recorded"`
	AdherenceRate    float64 `json:"adherence_rate"` // fully taken / expected, e.g. 85.7 (%)
}

func (h *ExportHandler) ExportDoctorSummary(w http.ResponseWriter, r *http.Request) {
	daysStr := r.URL.Query().Get("days")
	days := 7
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 && d <= 90 {
		days = d
	}

	// The last N household-local days, today included. Timestamps are stored in UTC,
	// so filter from local midnight converted to UTC (not a bare local date string).
	now := time.Now()
	loc := ClinicalLocation()
	endDate := now.In(loc)
	y, mo, d := endDate.Date()
	startDate := time.Date(y, mo, d, 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")
	sinceUTC := startDate.UTC().Format(sqliteTimeLayout)

	// 1. Vitals stats
	rows, err := h.DB.Query(`
		SELECT v.id, v.caregiver_id, v.recorded_at, v.systolic, v.diastolic, v.pulse, v.blood_sugar, v.alertness_level, v.vital_type, v.notes,
		       c.name, c.avatar_color, v.suspect_default_sugar, COALESCE(v.fast_result, ''), COALESCE(v.last_normal_at, ''),
		       COALESCE(v.supersedes_id, ''), o.systolic, o.diastolic, o.pulse, o.blood_sugar, o.alertness_level, COALESCE(oc.name, ''),
		       COALESCE(v.correction_reason, ''), COALESCE(v.corrected_at, ''),
		       v.spo2, v.temperature_c, o.spo2, o.temperature_c
		FROM vitals_logs v
		JOIN caregivers c ON v.caregiver_id = c.id
		LEFT JOIN vitals_logs o ON o.id = v.supersedes_id
		LEFT JOIN caregivers oc ON oc.id = o.caregiver_id
		WHERE v.recorded_at >= ? AND v.superseded_by IS NULL -- a corrected reading is replaced by its correction
		ORDER BY v.recorded_at DESC
	`, sinceUTC)
	if err != nil {
		internalError(w, "failed to load vitals for the report", err)
		return
	}

	var stats VitalsStats
	alertnessMap := map[string]int{"alert": 0, "confused": 0, "restless": 0, "drowsy": 0, "not_assessed": 0}
	recentVitals := []models.VitalsLog{} // never null in JSON: the report maps over it
	var sumSys, sumDia, sumPulse, sumSugar float64
	var bpCount, pulseCount, sugarCount int
	type bpDay struct{ sumSys, sumDia, n, maxSys, minSys int }
	bpDays := map[string]*bpDay{}
	oxyDays := map[string]*DailyOxygenTemp{}

	{
		defer rows.Close()
		firstBP := true
		for rows.Next() {
			var v models.VitalsLog
			var sys, dia, pulse sql.NullInt64
			var notes, vitalType sql.NullString
			var sugar sql.NullFloat64
			var recordedAtStr string
			var fastResult, lastNormal string
			var corr correctionScan
			var spo2 sql.NullInt64
			var tempC sql.NullFloat64
			if err := rows.Scan(&v.ID, &v.CaregiverID, &recordedAtStr, &sys, &dia, &pulse, &sugar, &v.AlertnessLevel, &vitalType, &notes, &v.CaregiverName, &v.AvatarColor, &v.SuspectDefaultSugar, &fastResult, &lastNormal,
				&corr.id, &corr.sys, &corr.dia, &corr.pulse, &corr.sugar, &corr.alertness, &corr.by, &corr.reason, &corr.at,
				&spo2, &tempC, &corr.spo2, &corr.temp); err != nil {
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

				if t, err := time.Parse(sqliteTimeLayout, recordedAtStr); err == nil {
					day := t.In(loc).Format("2006-01-02")
					b := bpDays[day]
					if b == nil {
						b = &bpDay{minSys: s}
						bpDays[day] = b
					}
					b.sumSys += s
					b.sumDia += d
					b.n++
					b.maxSys = max(b.maxSys, s)
					b.minSys = min(b.minSys, s)
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
			v.Correction = corr.toModel()
			setOxygenTemp(&v, spo2, tempC)
			if v.SpO2 != nil || v.TemperatureC != nil {
				if v.SpO2 != nil {
					if stats.MinSpO2 == nil || *v.SpO2 < *stats.MinSpO2 {
						n := *v.SpO2
						stats.MinSpO2 = &n
					}
					switch v.SpO2Status {
					case "CRITICAL":
						stats.CriticalSpO2Count++
						stats.LowSpO2Count++
					case "LOW":
						stats.LowSpO2Count++
					}
				}
				if v.TemperatureC != nil {
					if stats.MaxTemperatureC == nil || *v.TemperatureC > *stats.MaxTemperatureC {
						t := *v.TemperatureC
						stats.MaxTemperatureC = &t
					}
					if v.TempStatus == "FEVER" {
						stats.FeverCount++
					}
				}
				if t, err := time.Parse(sqliteTimeLayout, recordedAtStr); err == nil {
					day := t.In(loc).Format("2006-01-02")
					d := oxyDays[day]
					if d == nil {
						d = &DailyOxygenTemp{Date: day}
						oxyDays[day] = d
					}
					d.Readings++
					if v.SpO2 != nil && (d.MinSpO2 == nil || *v.SpO2 < *d.MinSpO2) {
						n := *v.SpO2
						d.MinSpO2 = &n
					}
					if v.TemperatureC != nil && (d.MaxTemperatureC == nil || *v.TemperatureC > *d.MaxTemperatureC) {
						t := *v.TemperatureC
						d.MaxTemperatureC = &t
					}
				}
			}
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
		if err := rows.Err(); err != nil {
			internalError(w, "failed to read vitals for the report", err)
			return
		}
	}
	dailyBP := []DailyBP{}
	dailyOxy := []DailyOxygenTemp{}
	for day := startDate; !day.After(endDate); day = day.AddDate(0, 0, 1) {
		key := day.Format("2006-01-02")
		if d := oxyDays[key]; d != nil {
			dailyOxy = append(dailyOxy, *d)
		}
		if b := bpDays[key]; b != nil {
			dailyBP = append(dailyBP, DailyBP{Date: key, AvgSys: float64(b.sumSys) / float64(b.n), AvgDia: float64(b.sumDia) / float64(b.n), MaxSys: b.maxSys, MinSys: b.minSys, Readings: b.n})
		}
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

	// 2. Medication adherence against expected doses (issue #14).
	perMedication, adherence, err := buildDoseReport(h.DB, startDate, endDate, now)
	if err != nil {
		internalError(w, "failed to compute adherence", err)
		return
	}

	// 3. Recent Handover notes
	noteRows, err := h.DB.Query(handoverNoteSelect+`
		WHERE n.created_at >= ?
		ORDER BY n.created_at DESC
		LIMIT 15
	`, sinceUTC)
	if err != nil {
		internalError(w, "failed to load notes for the report", err)
		return
	}

	recentNotes := []models.HandoverNote{}
	if noteRows != nil {
		defer noteRows.Close()
		for noteRows.Next() {
			if n, err := scanHandoverNote(noteRows); err == nil {
				recentNotes = append(recentNotes, n)
			}
		}
	}

	// 4. Insulin (the vitals/notes cursors above are closed, so the connection is free)
	if noteRows != nil {
		noteRows.Close()
	}
	insulinLogs, err := insulinLogsSince(h.DB, sinceUTC)
	if err != nil {
		internalError(w, "failed to load insulin for the report", err)
		return
	}

	// 5. Daily fluid intake (issue #28)
	dailyFluid := []DailyFluid{}
	targetML := currentFluidDailyTarget()
	fluidDays := map[string]*DailyFluid{}
	fluidRows, err := h.DB.Query(`
		SELECT value, COALESCE(thickened, 0), created_at
		FROM care_events
		WHERE kind = 'fluid' AND datetime(created_at) >= datetime(?)
		ORDER BY created_at ASC
	`, sinceUTC)
	if err == nil {
		defer fluidRows.Close()
		for fluidRows.Next() {
			var val, createdAt string
			var thick int
			if err := fluidRows.Scan(&val, &thick, &createdAt); err == nil {
				if t, err := parseDBTime(createdAt); err == nil {
					day := t.In(loc).Format("2006-01-02")
					d := fluidDays[day]
					if d == nil {
						d = &DailyFluid{Date: day, TargetML: targetML}
						fluidDays[day] = d
					}
					d.Entries++
					if ml, err := strconv.Atoi(val); err == nil && ml > 0 {
						d.TotalML += ml
						if thick == 1 {
							d.ThickenedML += ml
						}
					}
				}
			}
		}
	}
	// Walk from startDate to endDate
	for dt := startDate; !dt.After(endDate); dt = dt.AddDate(0, 0, 1) {
		dayStr := dt.Format("2006-01-02")
		d := fluidDays[dayStr]
		if d == nil {
			d = &DailyFluid{Date: dayStr, TargetML: targetML}
		}
		if targetML > 0 {
			d.PercentOfTarget = int(float64(d.TotalML) / float64(targetML) * 100)
		}
		dailyFluid = append(dailyFluid, *d)
	}

	// 6. Bowel movements and longest gap (issue #28)
	var bowel BowelSummary
	bowelRows, err := h.DB.Query(`
		SELECT value, created_at
		FROM care_events
		WHERE kind = 'bowel'
		ORDER BY created_at ASC
	`)
	if err == nil {
		defer bowelRows.Close()
		var prevTime time.Time
		hasPrev := false
		var lastTime time.Time
		hasLast := false
		for bowelRows.Next() {
			var val, createdAt string
			if err := bowelRows.Scan(&val, &createdAt); err == nil {
				if t, err := parseDBTime(createdAt); err == nil {
					bowel.TotalMovements++
					switch strings.ToLower(val) {
					case "hard":
						bowel.HardCount++
					case "normal":
						bowel.NormalCount++
					case "loose":
						bowel.LooseCount++
					}
					if hasPrev {
						gap := int(t.Sub(prevTime).Hours() / 24)
						if gap > bowel.LongestGapDays {
							bowel.LongestGapDays = gap
						}
					}
					prevTime = t
					hasPrev = true
					lastTime = t
					hasLast = true
				}
			}
		}
		if hasLast {
			lastStr := lastTime.UTC().Format(time.RFC3339)
			bowel.LastMovementAt = &lastStr
			daysSince := int(time.Now().Sub(lastTime).Hours() / 24)
			bowel.DaysSinceLast = &daysSince
			if daysSince > bowel.LongestGapDays {
				bowel.LongestGapDays = daysSince
			}
		}
	}

	res := DoctorSummaryResponse{
		GeneratedAt:     time.Now().UTC(),
		DaysCovered:     days,
		StartDate:       startDateStr,
		EndDate:         endDateStr,
		VitalsStats:     stats,
		Adherence:       adherence,
		PerMedication:   perMedication,
		DailyBP:         dailyBP,
		DailyOxygenTemp: dailyOxy,
		DailyFluid:      dailyFluid,
		BowelSummary:    bowel,
		Alertness:       alertnessMap,
		RecentNotes:     recentNotes,
		RecentVitals:    recentVitals,
		InsulinLogs:     insulinLogs,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}
