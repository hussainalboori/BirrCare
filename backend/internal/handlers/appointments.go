package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/sse"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Appointments and questions for the doctor (issue #63). Neurology follow-ups,
// blood tests and physio live in one list with who takes her; the one assigned
// gets a reminder the evening before and 2 hours before (appointment_watcher.go).
// Questions are added in one line whenever someone thinks of one, shown first in
// the doctor's report, and closed after the visit with what the doctor said.

type AppointmentHandler struct {
	DB     *sql.DB
	Broker *sse.Broker
}

type Appointment struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"` // doctor | lab | physio | other
	Title        string `json:"title"`
	StartsAt     string `json:"starts_at"` // RFC3339 UTC
	Date         string `json:"date"`      // household YYYY-MM-DD
	Time         string `json:"time"`      // household HH:MM
	Location     string `json:"location,omitempty"`
	Notes        string `json:"notes,omitempty"`
	AssignedID   string `json:"assigned_caregiver_id,omitempty"`
	AssignedName string `json:"assigned_name,omitempty"`
	Status       string `json:"status"` // scheduled | done | cancelled
	CreatedBy    string `json:"created_by_name,omitempty"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
	startsTime   time.Time
	createdTime  time.Time
	updatedTime  time.Time
}

type appointmentRequest struct {
	Kind            string `json:"kind"`
	Title           string `json:"title"`
	Date            string `json:"date"` // household YYYY-MM-DD
	Time            string `json:"time"` // household HH:MM
	Location        string `json:"location"`
	Notes           string `json:"notes"`
	AssignedID      string `json:"assigned_caregiver_id"`
	ClientRequestID string `json:"client_request_id"`
}

var hhmmPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

const (
	maxAppointmentTitle    = 120
	maxAppointmentLocation = 200
	maxQuestionLen         = 500
)

const appointmentSelect = `SELECT a.id, a.kind, a.title, a.starts_at, a.location, a.notes, COALESCE(a.assigned_caregiver_id, ''), COALESCE(g.name, ''),
	a.status, COALESCE(c.name, ''), a.created_at, a.updated_at
	FROM appointments a LEFT JOIN caregivers g ON g.id = a.assigned_caregiver_id LEFT JOIN caregivers c ON c.id = a.created_by`

func scanAppointments(rows *sql.Rows) ([]Appointment, error) {
	defer rows.Close()
	out := []Appointment{}
	loc := ClinicalLocation()
	for rows.Next() {
		var a Appointment
		if err := rows.Scan(&a.ID, &a.Kind, &a.Title, &a.StartsAt, &a.Location, &a.Notes, &a.AssignedID, &a.AssignedName,
			&a.Status, &a.CreatedBy, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		a.startsTime, _ = time.Parse(sqliteTimeLayout, a.StartsAt)
		a.createdTime, _ = time.Parse(sqliteTimeLayout, a.CreatedAt)
		a.updatedTime, _ = time.Parse(sqliteTimeLayout, a.UpdatedAt)
		a.Date = a.startsTime.In(loc).Format("2006-01-02")
		a.Time = a.startsTime.In(loc).Format("15:04")
		a.StartsAt, a.CreatedAt, a.UpdatedAt = sqliteToRFC3339(a.StartsAt), sqliteToRFC3339(a.CreatedAt), sqliteToRFC3339(a.UpdatedAt)
		out = append(out, a)
	}
	return out, rows.Err()
}

func appointmentByID(q querier, where string, arg string) (Appointment, error) {
	rows, err := q.Query(appointmentSelect+" WHERE "+where, arg)
	if err != nil {
		return Appointment{}, err
	}
	list, err := scanAppointments(rows)
	if err != nil {
		return Appointment{}, err
	}
	if len(list) == 0 {
		return Appointment{}, sql.ErrNoRows
	}
	return list[0], nil
}

// validate checks a create/edit request and returns the start moment.
func (req *appointmentRequest) validate(q querier, now time.Time, isNew bool) (time.Time, string, string) {
	req.Title, req.Location, req.Notes = strings.TrimSpace(req.Title), strings.TrimSpace(req.Location), strings.TrimSpace(req.Notes)
	switch req.Kind {
	case "doctor", "lab", "physio", "other":
	default:
		return time.Time{}, "INVALID_KIND", "kind must be doctor, lab, physio or other"
	}
	if req.Title == "" || len([]rune(req.Title)) > maxAppointmentTitle {
		return time.Time{}, "INVALID_TITLE", "write a short title (up to 120 letters)"
	}
	if len([]rune(req.Location)) > maxAppointmentLocation || len(req.Notes) > maxNotesLen {
		return time.Time{}, "NOTES_TOO_LONG", "location or notes are too long"
	}
	if !hhmmPattern.MatchString(req.Time) {
		return time.Time{}, "INVALID_TIME", "time must be HH:MM"
	}
	starts, err := time.ParseInLocation("2006-01-02 15:04", req.Date+" "+req.Time, ClinicalLocation())
	if err != nil {
		return time.Time{}, "INVALID_DATE", "date must be YYYY-MM-DD"
	}
	// A new appointment is in the future (or earlier today, entered late); years ahead is a typo.
	if isNew && req.Date < now.In(ClinicalLocation()).Format("2006-01-02") {
		return time.Time{}, "INVALID_DATE", "the appointment date is in the past"
	}
	if starts.After(now.AddDate(2, 0, 0)) {
		return time.Time{}, "INVALID_DATE", "the appointment is more than two years away"
	}
	if req.AssignedID != "" {
		var n int
		if err := q.QueryRow(`SELECT COUNT(*) FROM caregivers WHERE id = ? AND is_active = 1 AND role != 'viewer'`, req.AssignedID).Scan(&n); err != nil || n == 0 {
			return time.Time{}, "INVALID_CAREGIVER", "choose a family member who can take her"
		}
	}
	return starts, "", ""
}

// List: GET /appointments → {upcoming, recent}. Upcoming is everything still
// scheduled (including one that started in the last few hours); recent is the
// last 90 days of done, cancelled or passed appointments, newest first.
func (h *AppointmentHandler) List(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	cutoff := now.Add(-6 * time.Hour).Format(sqliteTimeLayout)
	rows, err := h.DB.Query(appointmentSelect+` WHERE a.status = 'scheduled' AND a.starts_at >= ? ORDER BY a.starts_at`, cutoff)
	if err != nil {
		internalError(w, "failed to load appointments", err)
		return
	}
	upcoming, err := scanAppointments(rows)
	if err != nil {
		internalError(w, "failed to load appointments", err)
		return
	}
	rows, err = h.DB.Query(appointmentSelect+` WHERE (a.status != 'scheduled' OR a.starts_at < ?) AND a.starts_at >= ? ORDER BY a.starts_at DESC LIMIT 30`,
		cutoff, now.AddDate(0, 0, -90).Format(sqliteTimeLayout))
	if err != nil {
		internalError(w, "failed to load appointments", err)
		return
	}
	recent, err := scanAppointments(rows)
	if err != nil {
		internalError(w, "failed to load appointments", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"upcoming": upcoming, "recent": recent})
}

// Create: POST /appointments.
func (h *AppointmentHandler) Create(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot add appointments")
		return
	}
	var req appointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if req.ClientRequestID != "" {
		if dup, err := appointmentByID(h.DB, "a.client_request_id = ?", req.ClientRequestID); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dup)
			return
		}
	}
	now := time.Now().UTC()
	starts, code, msg := req.validate(h.DB, now, true)
	if code != "" {
		writeJSONError(w, http.StatusBadRequest, code, msg)
		return
	}
	id := uuid.New().String()
	stamp := now.Format(sqliteTimeLayout)
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO appointments (id, kind, title, starts_at, location, notes, assigned_caregiver_id, status, created_by, created_at, updated_at, client_request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'scheduled', ?, ?, ?, ?)`, id, req.Kind, req.Title, starts.UTC().Format(sqliteTimeLayout), req.Location, req.Notes,
		nullIfEmpty(req.AssignedID), me.ID, stamp, stamp, nullIfEmpty(req.ClientRequestID)); err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	if err := audit.Record(tx, me.ID, "APPOINTMENT_ADDED", "appointment", id,
		fmt.Sprintf("%s %q on %s %s, assigned to %s", req.Kind, req.Title, req.Date, req.Time, req.AssignedID)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	appt, err := appointmentByID(tx, "a.id = ?", id)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	h.Broker.Broadcast("APPOINTMENT_CHANGED", actorOf(me), map[string]interface{}{"id": id, "status": appt.Status})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(appt)
}

// Update: PUT /appointments/{id} changes a scheduled appointment. A new time
// means new reminders (they are keyed by the start time).
func (h *AppointmentHandler) Update(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot change appointments")
		return
	}
	id := chi.URLParam(r, "id")
	var req appointmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	old, err := appointmentByID(h.DB, "a.id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "appointment not found")
		return
	} else if err != nil {
		internalError(w, "failed to load the appointment", err)
		return
	}
	if old.Status != "scheduled" {
		writeJSONError(w, http.StatusConflict, "NOT_SCHEDULED", "only a scheduled appointment can be changed")
		return
	}
	now := time.Now().UTC()
	starts, code, msg := req.validate(h.DB, now, false)
	if code != "" {
		writeJSONError(w, http.StatusBadRequest, code, msg)
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE appointments SET kind = ?, title = ?, starts_at = ?, location = ?, notes = ?, assigned_caregiver_id = ?, updated_at = ? WHERE id = ?`,
		req.Kind, req.Title, starts.UTC().Format(sqliteTimeLayout), req.Location, req.Notes, nullIfEmpty(req.AssignedID), now.Format(sqliteTimeLayout), id); err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	if err := audit.Record(tx, me.ID, "APPOINTMENT_CHANGED", "appointment", id,
		fmt.Sprintf("was %s %q on %s %s (assigned %s); now %s %q on %s %s (assigned %s)", old.Kind, old.Title, old.Date, old.Time, old.AssignedID,
			req.Kind, req.Title, req.Date, req.Time, req.AssignedID)); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	// Reminders already sent for the old time no longer apply.
	if !starts.Equal(old.startsTime) || req.AssignedID != old.AssignedID {
		if err := resolveAppointmentReminders(tx, id, now); err != nil {
			internalError(w, "failed to update reminders", err)
			return
		}
	}
	appt, err := appointmentByID(tx, "a.id = ?", id)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	h.Broker.Broadcast("APPOINTMENT_CHANGED", actorOf(me), map[string]interface{}{"id": id, "status": appt.Status})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(appt)
}

// SetStatus: POST /appointments/{id}/status {status: done|cancelled|scheduled}.
func (h *AppointmentHandler) SetStatus(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot change appointments")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	switch req.Status {
	case "done", "cancelled", "scheduled":
	default:
		writeJSONError(w, http.StatusBadRequest, "INVALID_STATUS", "status must be done, cancelled or scheduled")
		return
	}
	old, err := appointmentByID(h.DB, "a.id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "appointment not found")
		return
	} else if err != nil {
		internalError(w, "failed to load the appointment", err)
		return
	}
	now := time.Now().UTC()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	defer tx.Rollback()
	if old.Status != req.Status {
		if _, err := tx.Exec(`UPDATE appointments SET status = ?, updated_at = ? WHERE id = ?`, req.Status, now.Format(sqliteTimeLayout), id); err != nil {
			internalError(w, "failed to save the appointment", err)
			return
		}
		if err := audit.Record(tx, me.ID, "APPOINTMENT_"+strings.ToUpper(req.Status), "appointment", id,
			fmt.Sprintf("%s %q on %s %s: %s → %s", old.Kind, old.Title, old.Date, old.Time, old.Status, req.Status)); err != nil {
			internalError(w, "failed to record audit log", err)
			return
		}
		if req.Status != "scheduled" {
			if err := resolveAppointmentReminders(tx, id, now); err != nil {
				internalError(w, "failed to update reminders", err)
				return
			}
		}
	}
	appt, err := appointmentByID(tx, "a.id = ?", id)
	if err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the appointment", err)
		return
	}
	h.Broker.Broadcast("APPOINTMENT_CHANGED", actorOf(me), map[string]interface{}{"id": id, "status": appt.Status})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(appt)
}

// resolveAppointmentReminders clears an appointment's open reminder alerts.
func resolveAppointmentReminders(tx *sql.Tx, id string, now time.Time) error {
	_, err := tx.Exec(`UPDATE alerts SET resolved_at = ? WHERE kind = 'appointment_reminder' AND resolved_at IS NULL AND ref_id LIKE ?`,
		now.UTC().Format(sqliteTimeLayout), id+":%")
	return err
}

// ICS: GET /appointments/{id}/ics, an "Add to my calendar" file (RFC 5545).
func (h *AppointmentHandler) ICS(w http.ResponseWriter, r *http.Request) {
	appt, err := appointmentByID(h.DB, "a.id = ?", chi.URLParam(r, "id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "appointment not found")
		return
	} else if err != nil {
		internalError(w, "failed to load the appointment", err)
		return
	}
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="birrcare-appointment.ics"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(appointmentICS(appt, time.Now().UTC())))
}

// appointmentICS renders one appointment as an iCalendar file. Times are UTC
// ("Z"), so every calendar shows them at the right moment in its own zone. The
// SEQUENCE grows with each change, so re-importing an edited appointment
// updates the event instead of adding a second one.
func appointmentICS(a Appointment, now time.Time) string {
	kinds := map[string]string{"doctor": "Doctor", "lab": "Lab test", "physio": "Physiotherapy", "other": "Appointment"}
	desc := kinds[a.Kind]
	if a.AssignedName != "" {
		desc += "\nTakes her: " + a.AssignedName
	}
	if a.Notes != "" {
		desc += "\n" + a.Notes
	}
	status := "CONFIRMED"
	if a.Status == "cancelled" {
		status = "CANCELLED"
	}
	seq := 0
	if !a.updatedTime.IsZero() && a.updatedTime.After(a.createdTime) {
		seq = int(a.updatedTime.Sub(a.createdTime).Seconds())
	}
	const icsTime = "20060102T150405Z"
	lines := []string{
		"BEGIN:VCALENDAR",
		"VERSION:2.0",
		"PRODID:-//Birrcare//Appointments//EN",
		"CALSCALE:GREGORIAN",
		"METHOD:PUBLISH",
		"BEGIN:VEVENT",
		"UID:" + a.ID + "@birrcare",
		"DTSTAMP:" + now.UTC().Format(icsTime),
		"DTSTART:" + a.startsTime.UTC().Format(icsTime),
		"DTEND:" + a.startsTime.Add(time.Hour).UTC().Format(icsTime),
		fmt.Sprintf("SEQUENCE:%d", seq),
		"STATUS:" + status,
		"SUMMARY:" + icsText(a.Title),
	}
	if a.Location != "" {
		lines = append(lines, "LOCATION:"+icsText(a.Location))
	}
	lines = append(lines, "DESCRIPTION:"+icsText(desc),
		"BEGIN:VALARM", "ACTION:DISPLAY", "DESCRIPTION:"+icsText(a.Title), "TRIGGER:-PT2H", "END:VALARM",
		"END:VEVENT", "END:VCALENDAR")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(icsFold(l))
		b.WriteString("\r\n")
	}
	return b.String()
}

// icsText escapes a TEXT value (RFC 5545 §3.3.11).
func icsText(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`, "\r", "").Replace(s)
}

// icsFold folds a content line at 75 octets without splitting a UTF-8 letter
// (Arabic letters are two bytes each).
func icsFold(line string) string {
	var b strings.Builder
	n := 0
	for _, r := range line {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

// ---- Questions for the doctor ----

type DoctorQuestion struct {
	ID            string `json:"id"`
	Text          string `json:"text"`
	By            string `json:"by,omitempty"`
	CreatedByID   string `json:"created_by,omitempty"`
	CreatedAt     string `json:"created_at"`
	AnsweredAt    string `json:"answered_at,omitempty"`
	AnsweredBy    string `json:"answered_by_name,omitempty"`
	Answer        string `json:"answer,omitempty"`
	AppointmentID string `json:"appointment_id,omitempty"`
}

const questionSelect = `SELECT q.id, q.text, COALESCE(c.name, ''), q.created_by, q.created_at, COALESCE(q.answered_at, ''), COALESCE(a.name, ''), q.answer, COALESCE(q.appointment_id, '')
	FROM doctor_questions q LEFT JOIN caregivers c ON c.id = q.created_by LEFT JOIN caregivers a ON a.id = q.answered_by`

func scanQuestions(rows *sql.Rows) ([]DoctorQuestion, error) {
	defer rows.Close()
	out := []DoctorQuestion{}
	for rows.Next() {
		var q DoctorQuestion
		if err := rows.Scan(&q.ID, &q.Text, &q.By, &q.CreatedByID, &q.CreatedAt, &q.AnsweredAt, &q.AnsweredBy, &q.Answer, &q.AppointmentID); err != nil {
			return nil, err
		}
		q.CreatedAt = sqliteToRFC3339(q.CreatedAt)
		if q.AnsweredAt != "" {
			q.AnsweredAt = sqliteToRFC3339(q.AnsweredAt)
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// openQuestions lists the questions not yet answered, oldest first.
func openQuestions(q querier) ([]DoctorQuestion, error) {
	rows, err := q.Query(questionSelect + ` WHERE q.answered_at IS NULL AND q.removed_at IS NULL ORDER BY q.created_at`)
	if err != nil {
		return nil, err
	}
	return scanQuestions(rows)
}

// answeredQuestionsSince lists answers given since a UTC moment, newest first.
func answeredQuestionsSince(q querier, since time.Time) ([]DoctorQuestion, error) {
	rows, err := q.Query(questionSelect+` WHERE q.answered_at IS NOT NULL AND q.removed_at IS NULL AND q.answered_at >= ? ORDER BY q.answered_at DESC`,
		since.UTC().Format(sqliteTimeLayout))
	if err != nil {
		return nil, err
	}
	return scanQuestions(rows)
}

func questionByID(q querier, where, arg string) (DoctorQuestion, error) {
	rows, err := q.Query(questionSelect+" WHERE "+where, arg)
	if err != nil {
		return DoctorQuestion{}, err
	}
	list, err := scanQuestions(rows)
	if err != nil {
		return DoctorQuestion{}, err
	}
	if len(list) == 0 {
		return DoctorQuestion{}, sql.ErrNoRows
	}
	return list[0], nil
}

// ListQuestions: GET /doctor-questions → {open, answered (last 180 days)}.
func (h *AppointmentHandler) ListQuestions(w http.ResponseWriter, r *http.Request) {
	open, err := openQuestions(h.DB)
	if err != nil {
		internalError(w, "failed to load questions", err)
		return
	}
	answered, err := answeredQuestionsSince(h.DB, time.Now().AddDate(0, 0, -180))
	if err != nil {
		internalError(w, "failed to load questions", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"open": open, "answered": answered})
}

// AddQuestion: POST /doctor-questions {text}.
func (h *AppointmentHandler) AddQuestion(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot add questions")
		return
	}
	var req struct {
		Text            string `json:"text"`
		ClientRequestID string `json:"client_request_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len([]rune(req.Text)) > maxQuestionLen {
		writeJSONError(w, http.StatusBadRequest, "INVALID_TEXT", "write the question (up to 500 letters)")
		return
	}
	if req.ClientRequestID != "" {
		if dup, err := questionByID(h.DB, "q.client_request_id = ?", req.ClientRequestID); err == nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(dup)
			return
		}
	}
	id := uuid.New().String()
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the question", err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`INSERT INTO doctor_questions (id, text, created_by, created_at, client_request_id) VALUES (?, ?, ?, ?, ?)`,
		id, req.Text, me.ID, time.Now().UTC().Format(sqliteTimeLayout), nullIfEmpty(req.ClientRequestID)); err != nil {
		internalError(w, "failed to save the question", err)
		return
	}
	if err := audit.Record(tx, me.ID, "DOCTOR_QUESTION_ADDED", "doctor_question", id, req.Text); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	q, err := questionByID(tx, "q.id = ?", id)
	if err != nil {
		internalError(w, "failed to save the question", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the question", err)
		return
	}
	h.Broker.Broadcast("DOCTOR_QUESTION_CHANGED", actorOf(me), map[string]interface{}{"id": id})
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(q)
}

// AnswerQuestion: POST /doctor-questions/{id}/answer {answer, appointment_id}.
// "Doctor said…" closes the question; it stays in the history.
func (h *AppointmentHandler) AnswerQuestion(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot answer questions")
		return
	}
	id := chi.URLParam(r, "id")
	var req struct {
		Answer        string `json:"answer"`
		AppointmentID string `json:"appointment_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	req.Answer = strings.TrimSpace(req.Answer)
	if req.Answer == "" || len(req.Answer) > maxNotesLen {
		writeJSONError(w, http.StatusBadRequest, "INVALID_ANSWER", "write what the doctor said")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save the answer", err)
		return
	}
	defer tx.Rollback()
	if req.AppointmentID != "" {
		if _, err := appointmentByID(tx, "a.id = ?", req.AppointmentID); err != nil {
			writeJSONError(w, http.StatusBadRequest, "INVALID_APPOINTMENT", "appointment not found")
			return
		}
	}
	res, err := tx.Exec(`UPDATE doctor_questions SET answer = ?, answered_at = ?, answered_by = ?, appointment_id = COALESCE(?, appointment_id)
		WHERE id = ? AND answered_at IS NULL AND removed_at IS NULL`,
		req.Answer, time.Now().UTC().Format(sqliteTimeLayout), me.ID, nullIfEmpty(req.AppointmentID), id)
	if err != nil {
		internalError(w, "failed to save the answer", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSONError(w, http.StatusConflict, "NOT_OPEN", "this question is already answered or removed")
		return
	}
	if err := audit.Record(tx, me.ID, "DOCTOR_QUESTION_ANSWERED", "doctor_question", id, req.Answer); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	q, err := questionByID(tx, "q.id = ?", id)
	if err != nil {
		internalError(w, "failed to save the answer", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save the answer", err)
		return
	}
	h.Broker.Broadcast("DOCTOR_QUESTION_CHANGED", actorOf(me), map[string]interface{}{"id": id})
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}

// RemoveQuestion: DELETE /doctor-questions/{id}. An open question can be taken
// off the list by whoever wrote it or an admin; the row stays for the audit.
func (h *AppointmentHandler) RemoveQuestion(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	if !auth.CanTakeActions(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "Viewers cannot remove questions")
		return
	}
	id := chi.URLParam(r, "id")
	q, err := questionByID(h.DB, "q.id = ?", id)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSONError(w, http.StatusNotFound, "NOT_FOUND", "question not found")
		return
	} else if err != nil {
		internalError(w, "failed to load the question", err)
		return
	}
	if q.CreatedByID != me.ID && !auth.CanManageMedications(me) {
		writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "only the one who wrote it or an admin can remove a question")
		return
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to remove the question", err)
		return
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE doctor_questions SET removed_at = ? WHERE id = ? AND answered_at IS NULL AND removed_at IS NULL`,
		time.Now().UTC().Format(sqliteTimeLayout), id)
	if err != nil {
		internalError(w, "failed to remove the question", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSONError(w, http.StatusConflict, "NOT_OPEN", "this question is already answered or removed")
		return
	}
	if err := audit.Record(tx, me.ID, "DOCTOR_QUESTION_REMOVED", "doctor_question", id, q.Text); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to remove the question", err)
		return
	}
	h.Broker.Broadcast("DOCTOR_QUESTION_CHANGED", actorOf(me), map[string]interface{}{"id": id})
	w.WriteHeader(http.StatusNoContent)
}
