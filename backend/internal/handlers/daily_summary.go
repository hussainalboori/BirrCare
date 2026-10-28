package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"birrcare/backend/internal/audit"
	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/models"
	"birrcare/backend/internal/push"
)

// The daily family summary (issue #67). A brother abroad calls every evening
// "how was she today?"; this answers in ten seconds: medicines given, the blood
// pressure range and any alerts in plain words, meals and mood from the
// handovers, fluids, falls, who was on duty and a line from the last note.
// In the app it's the "Today" card; at the Super Admin's chosen time (default
// 21:00 at home) everyone who opted in gets one push. The lock screen shows no
// values unless that person turned on "show details on the lock screen".

const defaultSummaryTime = "21:00"

type DailySummary struct {
	Date           string   `json:"date"`
	Lang           string   `json:"lang"`
	PausedSince    string   `json:"paused_since,omitempty"` // RFC3339: "In hospital since …" and nothing else
	PauseReason    string   `json:"pause_reason,omitempty"`
	MedsExpected   int      `json:"meds_expected"`
	MedsGiven      int      `json:"meds_given"`
	Missed         []string `json:"missed"`
	BloodThinnerOK *bool    `json:"blood_thinner_ok,omitempty"`
	BPCount        int      `json:"bp_count"`
	BPMinSys       int      `json:"bp_min_systolic,omitempty"`
	BPMaxSys       int      `json:"bp_max_systolic,omitempty"`
	Alerts         []string `json:"alerts"`
	Notes          []string `json:"notes"` // meals, mood, sleep from handover tags
	FluidML        int      `json:"fluid_ml"`
	FluidTargetML  int      `json:"fluid_target_ml"`
	Incidents      []string `json:"incidents"`
	OnDuty         []string `json:"on_duty"`
	LastHandover   string   `json:"last_handover,omitempty"`
	Lines          []string `json:"lines"`
	Headline       string   `json:"headline"`
}

// Labels in the server's words, matching frontend/src/lib/handoverTags.ts.
var tagLabels = map[string][2]string{
	"ate_well": {"ate well", "أكلت جيداً"}, "ate_half": {"ate half", "أكلت النصف"}, "low_appetite": {"low appetite", "شهية ضعيفة"},
	"swallowing_difficulty": {"swallowing difficulty", "صعوبة في البلع"}, "needs_rest": {"needs rest", "تحتاج راحة"},
	"good_mood": {"good mood", "مزاج جيد"}, "agitated": {"agitated", "منفعلة"}, "pain": {"pain", "تشعر بألم"},
	"slept_well": {"slept well", "نامت جيداً"}, "poor_sleep": {"poor sleep", "نوم سيئ"}, "bowel_movement": {"bowel movement", "دخلت الحمام"},
}

// Health alerts in plain words; housekeeping alerts (stock, duty, backups) are left out.
var summaryAlertLabels = map[string][2]string{
	"bp_crisis": {"very high blood pressure", "ضغط مرتفع جداً"}, "bp_low": {"low blood pressure", "ضغط منخفض"},
	"fast_positive": {"stroke sign in the F.A.S.T. check", "علامة جلطة في فحص F.A.S.T."}, "alertness": {"hard to wake or confused", "صعوبة في الاستيقاظ أو تشوش"},
	"spo2_low": {"low oxygen", "أكسجين منخفض"}, "sugar_low": {"low blood sugar", "هبوط السكر"}, "sugar_very_high": {"very high blood sugar", "سكر مرتفع جداً"},
	"fever": {"fever", "حمّى"}, "fever_high": {"high fever", "حمّى شديدة"}, "bleeding": {"a bleeding sign", "علامة نزيف"},
	"vomited": {"vomited after a dose", "استفرغت بعد جرعة"}, "inr_out_of_range": {"INR outside the doctor's range", "INR خارج حدود الطبيب"},
}

func pick(l [2]string, ar bool) string {
	if ar {
		return l[1]
	}
	return l[0]
}

func addOnce(list []string, s string) []string {
	for _, x := range list {
		if x == s {
			return list
		}
	}
	return append(list, s)
}

// buildDailySummary summarises a household day as of now, in "ar" or "en".
func buildDailySummary(db *sql.DB, date, lang string, now time.Time) (*DailySummary, error) {
	loc := ClinicalLocation()
	ar := lang != "en"
	day, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, err
	}
	dayEnd := day.AddDate(0, 0, 1)
	until := dayEnd
	if now.Before(until) {
		until = now
	}
	s := &DailySummary{Date: date, Lang: map[bool]string{true: "ar", false: "en"}[ar], Missed: []string{}, Alerts: []string{}, Notes: []string{},
		Incidents: []string{}, OnDuty: []string{}, Lines: []string{}}

	// In hospital or away: only that (issue #59).
	if p, err := pausedAt(db, until.Add(-time.Second)); err != nil {
		return nil, err
	} else if p != nil {
		s.PausedSince, s.PauseReason = p.StartedAt, p.Reason
		since := p.startedTime.In(loc).Format("2 Jan")
		where := map[string][2]string{"hospital": {"In hospital", "في المستشفى"}, "travel": {"Away from home", "خارج المنزل"}}[p.Reason]
		if where[0] == "" {
			where = [2]string{"Care paused", "الرعاية متوقفة"}
		}
		if ar {
			s.Headline = fmt.Sprintf("%s منذ %s", where[1], since)
		} else {
			s.Headline = fmt.Sprintf("%s since %s", where[0], since)
		}
		s.Lines = []string{s.Headline}
		return s, nil
	}

	// Medicines.
	meds, _, err := buildDoseReport(db, day, day, now)
	if err != nil {
		return nil, err
	}
	for _, m := range meds {
		if m.Expected == 0 {
			continue
		}
		given := m.Taken + m.Partial
		s.MedsExpected += m.Expected
		s.MedsGiven += given
		name := m.NameEn
		if ar && m.NameAr != "" {
			name = m.NameAr
		}
		if given < m.Expected {
			s.Missed = append(s.Missed, name)
		}
		if m.IsCritical {
			ok := given >= m.Expected
			if s.BloodThinnerOK == nil || !ok {
				s.BloodThinnerOK = &ok
			}
		}
	}

	from, to := day.UTC().Format(sqliteTimeLayout), until.UTC().Format(sqliteTimeLayout)

	// Blood pressure range (a corrected reading counts as its correction).
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MIN(systolic), 0), COALESCE(MAX(systolic), 0) FROM vitals_logs
		WHERE systolic IS NOT NULL AND superseded_by IS NULL AND datetime(recorded_at) >= datetime(?) AND datetime(recorded_at) < datetime(?)`, from, to).
		Scan(&s.BPCount, &s.BPMinSys, &s.BPMaxSys); err != nil {
		return nil, err
	}

	// Health alerts for the family that day.
	rows, err := db.Query(`SELECT kind FROM alerts WHERE target_caregiver_id IS NULL AND datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?) ORDER BY created_at`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k string
		if rows.Scan(&k) == nil {
			if l, ok := summaryAlertLabels[k]; ok {
				s.Alerts = addOnce(s.Alerts, pick(l, ar))
			}
		}
	}
	rows.Close()

	// Handovers: tags and the last note.
	rows, err = db.Query(`SELECT COALESCE(tags, ''), summary FROM handover_notes WHERE datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?) ORDER BY datetime(created_at)`, from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var tags, summary string
		if rows.Scan(&tags, &summary) != nil {
			continue
		}
		var keys []string
		_ = json.Unmarshal([]byte(tags), &keys)
		for _, k := range keys {
			if l, ok := tagLabels[k]; ok {
				s.Notes = addOnce(s.Notes, pick(l, ar))
			}
		}
		if t := strings.TrimSpace(summary); t != "" {
			s.LastHandover = t
		}
	}
	rows.Close()
	if utf8.RuneCountInString(s.LastHandover) > 140 {
		s.LastHandover = string([]rune(s.LastHandover)[:139]) + "…"
	}

	// Fluids and incidents (care_events use RFC3339).
	s.FluidTargetML = currentFluidDailyTarget()
	rows, err = db.Query(`SELECT kind, value, created_at FROM care_events WHERE kind IN ('fluid', 'fall', 'seizure') AND datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?)`,
		from, to)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, value, at string
		if rows.Scan(&kind, &value, &at) != nil {
			continue
		}
		if kind == "fluid" {
			if ml, err := strconv.Atoi(value); err == nil && ml > 0 {
				s.FluidML += ml
			}
			continue
		}
		hhmm := ""
		if t, err := parseDBTime(at); err == nil {
			hhmm = t.In(loc).Format("15:04")
		}
		label := map[string][2]string{"fall": {"Fall at %s", "سقوط الساعة %s"}, "seizure": {"Seizure at %s", "نوبة تشنج الساعة %s"}}[kind]
		s.Incidents = append(s.Incidents, fmt.Sprintf(pick(label, ar), hhmm))
	}
	rows.Close()

	// Who was on duty.
	rows, err = db.Query(`SELECT DISTINCT c.name FROM shift_assignments a JOIN caregivers c ON c.id = a.caregiver_id WHERE a.date = ? ORDER BY c.name`, date)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			s.OnDuty = append(s.OnDuty, n)
		}
	}
	rows.Close()

	s.compose(ar)
	return s, nil
}

// compose writes the lines and the one-line headline.
func (s *DailySummary) compose(ar bool) {
	sep := map[bool]string{true: "، ", false: ", "}[ar]
	var head []string
	if s.MedsExpected > 0 {
		var line, short string
		switch {
		case s.MedsGiven >= s.MedsExpected && ar:
			line, short = "الأدوية: أُعطيت كلها", "أُعطيت كل الأدوية"
		case s.MedsGiven >= s.MedsExpected:
			line, short = "Medicines: all given", "all medicines given"
		case ar:
			short = fmt.Sprintf("أُعطيت %d من %d جرعات", s.MedsGiven, s.MedsExpected)
			line = fmt.Sprintf("الأدوية: %d من %d (لم تُسجَّل: %s)", s.MedsGiven, s.MedsExpected, strings.Join(s.Missed, sep))
		default:
			short = fmt.Sprintf("%d of %d doses given", s.MedsGiven, s.MedsExpected)
			line = fmt.Sprintf("Medicines: %d of %d given (not recorded: %s)", s.MedsGiven, s.MedsExpected, strings.Join(s.Missed, sep))
		}
		s.Lines = append(s.Lines, line)
		head = append(head, short)
		if s.BloodThinnerOK != nil {
			switch {
			case *s.BloodThinnerOK && ar:
				s.Lines = append(s.Lines, "مميّع الدم: أُعطي ✓")
			case *s.BloodThinnerOK:
				s.Lines = append(s.Lines, "Blood thinner: given ✓")
			case ar:
				s.Lines = append(s.Lines, "مميّع الدم: لم يُسجَّل كله ✗")
			default:
				s.Lines = append(s.Lines, "Blood thinner: not all recorded ✗")
			}
		}
	}
	if s.BPCount > 0 {
		bp := fmt.Sprintf("%d–%d", s.BPMinSys, s.BPMaxSys)
		if s.BPMinSys == s.BPMaxSys {
			bp = strconv.Itoa(s.BPMinSys)
		}
		if ar {
			s.Lines = append(s.Lines, "الضغط (الانقباضي): "+bp)
			head = append(head, "الضغط "+bp)
		} else {
			s.Lines = append(s.Lines, "Blood pressure (top number): "+bp)
			head = append(head, "BP "+bp)
		}
	}
	if len(s.Alerts) > 0 {
		s.Lines = append(s.Lines, map[bool]string{true: "تنبيهات: ", false: "Alerts: "}[ar]+strings.Join(s.Alerts, sep))
		head = append(head, s.Alerts[0])
	}
	if len(s.Notes) > 0 {
		s.Lines = append(s.Lines, map[bool]string{true: "الأكل والمزاج: ", false: "Meals and mood: "}[ar]+strings.Join(s.Notes, sep))
		head = append(head, s.Notes[0])
	}
	if s.FluidML > 0 {
		if ar {
			s.Lines = append(s.Lines, fmt.Sprintf("السوائل: %d من %d مل", s.FluidML, s.FluidTargetML))
		} else {
			s.Lines = append(s.Lines, fmt.Sprintf("Fluids: %d of %d ml", s.FluidML, s.FluidTargetML))
		}
	}
	s.Lines = append(s.Lines, s.Incidents...)
	if len(s.Incidents) > 0 {
		head = append(head, s.Incidents[0])
	}
	if len(s.OnDuty) > 0 {
		s.Lines = append(s.Lines, map[bool]string{true: "المناوب: ", false: "On duty: "}[ar]+strings.Join(s.OnDuty, sep))
	}
	if s.LastHandover != "" {
		s.Lines = append(s.Lines, map[bool]string{true: "آخر ملاحظة: ", false: "Last note: "}[ar]+"«"+s.LastHandover+"»")
	}
	if len(head) == 0 {
		s.Headline = map[bool]string{true: "اليوم: لا شيء مسجّل بعد", false: "Today: nothing recorded yet"}[ar]
		return
	}
	s.Headline = map[bool]string{true: "اليوم: ", false: "Today: "}[ar] + strings.Join(head, sep)
}

// summaryPushMessage is the lock-screen text: no values unless the person chose
// "show details on the lock screen" (anyone can see a lock screen).
func summaryPushMessage(s *DailySummary, details bool, lang string) push.Message {
	ar := lang != "en"
	body := map[bool]string{true: "ملخص اليوم جاهز — افتح رعاية بر", false: "Today's summary is ready — open Birrcare"}[ar]
	tag := "daily-summary"
	if s != nil {
		tag += "-" + s.Date
		if details {
			body = s.Headline
		}
	}
	return push.Message{Title: map[bool]string{true: "رعاية بر", false: "Birrcare"}[ar], Body: body, Tag: tag, URL: "/report"}
}

// ---- Settings and preferences ----

func summaryTime(q querier) string {
	var v string
	if err := q.QueryRow(`SELECT value FROM app_settings WHERE key = 'daily_summary_time'`).Scan(&v); err != nil || !hhmmPattern.MatchString(v) {
		return defaultSummaryTime
	}
	return v
}

type SummaryPrefs struct {
	Enabled     bool   `json:"enabled"`
	LockDetails bool   `json:"lock_details"`
	Time        string `json:"time"` // household HH:MM, set by the Super Admin
}

func summaryPrefs(q querier, cg string) (SummaryPrefs, error) {
	p := SummaryPrefs{Time: summaryTime(q)}
	err := q.QueryRow(`SELECT enabled, lock_details FROM daily_summary_prefs WHERE caregiver_id = ?`, cg).Scan(&p.Enabled, &p.LockDetails)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return p, err
}

type DailySummaryHandler struct {
	DB *sql.DB
}

// Today: GET /summary/today?date= — the "Today" card, in the reader's language.
func (h *DailySummaryHandler) Today(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	date := r.URL.Query().Get("date")
	if date == "" {
		date = clinicalToday()
	} else if canon, _, ok := parseClinicalDate(date); ok && canon <= clinicalToday() {
		date = canon
	} else {
		writeJSONError(w, http.StatusBadRequest, "INVALID_DATE", "date must be YYYY-MM-DD, not in the future")
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang != "ar" && lang != "en" {
		lang = me.PreferredLang
	}
	s, err := buildDailySummary(h.DB, date, lang, time.Now())
	if err != nil {
		internalError(w, "failed to build the summary", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s)
}

// GetPrefs: GET /summary/prefs — my opt-in, lock-screen choice and the send time.
func (h *DailySummaryHandler) GetPrefs(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	p, err := summaryPrefs(h.DB, me.ID)
	if err != nil {
		internalError(w, "failed to load preferences", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(p)
}

// PutPrefs: PUT /summary/prefs {enabled?, lock_details?, time?}. Anyone, viewers
// included, for themselves; only the Super Admin may change the time.
func (h *DailySummaryHandler) PutPrefs(w http.ResponseWriter, r *http.Request) {
	me := auth.GetCaregiverFromContext(r.Context())
	var req struct {
		Enabled     *bool   `json:"enabled"`
		LockDetails *bool   `json:"lock_details"`
		Time        *string `json:"time"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_PAYLOAD", "invalid request payload")
		return
	}
	if req.Time != nil {
		if me.Role != models.RoleSuperAdmin {
			writeJSONError(w, http.StatusForbidden, "FORBIDDEN", "only the Super Admin can change the summary time")
			return
		}
		if !hhmmPattern.MatchString(*req.Time) {
			writeJSONError(w, http.StatusBadRequest, "INVALID_TIME", "time must be HH:MM")
			return
		}
	}
	cur, err := summaryPrefs(h.DB, me.ID)
	if err != nil {
		internalError(w, "failed to load preferences", err)
		return
	}
	if req.Enabled != nil {
		cur.Enabled = *req.Enabled
	}
	if req.LockDetails != nil {
		cur.LockDetails = *req.LockDetails
	}
	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		internalError(w, "failed to save", err)
		return
	}
	defer tx.Rollback()
	ts := time.Now().UTC().Format(sqliteTimeLayout)
	if _, err := tx.Exec(`INSERT INTO daily_summary_prefs (caregiver_id, enabled, lock_details, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (caregiver_id) DO UPDATE SET enabled = excluded.enabled, lock_details = excluded.lock_details, updated_at = excluded.updated_at`,
		me.ID, cur.Enabled, cur.LockDetails, ts); err != nil {
		internalError(w, "failed to save", err)
		return
	}
	detail := fmt.Sprintf("daily summary enabled=%t lock_details=%t", cur.Enabled, cur.LockDetails)
	if req.Time != nil {
		if _, err := tx.Exec(`INSERT INTO app_settings (key, value) VALUES ('daily_summary_time', ?) ON CONFLICT (key) DO UPDATE SET value = excluded.value`, *req.Time); err != nil {
			internalError(w, "failed to save", err)
			return
		}
		cur.Time = *req.Time
		detail += " time=" + *req.Time
	}
	if err := audit.Record(tx, me.ID, "DAILY_SUMMARY_PREFS", "caregiver", me.ID, detail); err != nil {
		internalError(w, "failed to record audit log", err)
		return
	}
	if err := tx.Commit(); err != nil {
		internalError(w, "failed to save", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cur)
}

// ---- The nightly push ----

// sendFunc delivers one summary push; replaced in tests.
type summarySender func(caregiverID string, msg func(lang string) push.Message)

// CheckDailySummaries sends today's summary once to everyone who opted in, at
// or after the chosen household time.
func CheckDailySummaries(ctx context.Context, db *sql.DB, send summarySender, now time.Time) error {
	loc := ClinicalLocation()
	local := now.In(loc)
	if local.Format("15:04") < summaryTime(db) {
		return nil
	}
	date := local.Format("2006-01-02")
	rows, err := db.QueryContext(ctx, `SELECT p.caregiver_id, p.lock_details, COALESCE(c.preferred_lang, 'ar') FROM daily_summary_prefs p
		JOIN caregivers c ON c.id = p.caregiver_id
		WHERE p.enabled = 1 AND c.is_active = 1
		  AND NOT EXISTS (SELECT 1 FROM daily_summary_sent s WHERE s.caregiver_id = p.caregiver_id AND s.date = ?)`, date)
	if err != nil {
		return err
	}
	type who struct {
		id, lang string
		details  bool
	}
	var list []who
	for rows.Next() {
		var x who
		if rows.Scan(&x.id, &x.details, &x.lang) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	cache := map[string]*DailySummary{}
	for _, x := range list {
		res, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO daily_summary_sent (caregiver_id, date, sent_at) VALUES (?, ?, ?)`,
			x.id, date, now.UTC().Format(sqliteTimeLayout))
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // sent already
		}
		details := x.details
		summaries := map[string]*DailySummary{}
		for _, lang := range []string{"ar", "en"} {
			if cache[lang] == nil {
				s, err := buildDailySummary(db, date, lang, now)
				if err != nil {
					return err
				}
				cache[lang] = s
			}
			summaries[lang] = cache[lang]
		}
		send(x.id, func(lang string) push.Message {
			if lang != "en" {
				lang = "ar"
			}
			return summaryPushMessage(summaries[lang], details, lang)
		})
	}
	return nil
}

// StartDailySummaries checks every minute whether today's summary is due.
func StartDailySummaries(ctx context.Context, db *sql.DB, notifier *push.Notifier) {
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-t.C:
				if err := CheckDailySummaries(ctx, db, notifier.SendMessageAsync, now); err != nil {
					log.Printf("[SUMMARY] check failed: %v", err)
				}
			}
		}
	}()
}
