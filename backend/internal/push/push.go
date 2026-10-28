// Package push sends Web Push notifications (issue #12) so a crisis reaches a
// locked phone. Installed home-screen apps on iOS 16.4+ and Android Chrome
// support it. Only alerts are pushed, never routine events, and the payload is
// deliberately vague for the lock screen: no names, no values.
package push

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Notifier sends alert pushes to every subscribed caregiver except the one who
// caused the alert. A nil *Notifier is valid and does nothing (tests, no keys).
type Notifier struct {
	DB         *sql.DB
	PublicKey  string
	privateKey string
	subscriber string
	client     webpush.HTTPClient
}

// Init loads the VAPID key pair from app_settings, generating and storing it on
// first boot (like the session secret), so subscriptions survive restarts.
// PUSH_CONTACT (mailto: or https: URL) identifies the sender to push services.
func Init(db *sql.DB) (*Notifier, error) {
	pub, priv, err := loadKeys(db)
	if err != nil {
		return nil, err
	}
	subscriber := os.Getenv("PUSH_CONTACT")
	if subscriber == "" {
		subscriber = "https://github.com/hussainalboori/BirrCare"
	}
	return &Notifier{DB: db, PublicKey: pub, privateKey: priv, subscriber: subscriber, client: &http.Client{Timeout: 15 * time.Second}}, nil
}

func loadKeys(db *sql.DB) (string, string, error) {
	read := func() (string, string, error) {
		var pub, priv string
		err := db.QueryRow(`SELECT
			(SELECT value FROM app_settings WHERE key = 'vapid_public'),
			(SELECT value FROM app_settings WHERE key = 'vapid_private')`).Scan(&pub, &priv)
		return pub, priv, err
	}
	pub, priv, err := read()
	if err == nil && pub != "" && priv != "" {
		return pub, priv, nil
	}
	newPriv, newPub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		return "", "", fmt.Errorf("generating VAPID keys: %w", err)
	}
	if _, err := db.Exec(`INSERT OR IGNORE INTO app_settings (key, value) VALUES ('vapid_public', ?), ('vapid_private', ?)`, newPub, newPriv); err != nil {
		return "", "", fmt.Errorf("storing VAPID keys: %w", err)
	}
	pub, priv, err = read()
	if err != nil || pub == "" || priv == "" {
		return "", "", errors.New("VAPID keys missing after generation")
	}
	return pub, priv, nil
}

// SetHTTPClient replaces the client used to reach push services (tests, proxies).
func (n *Notifier) SetHTTPClient(c webpush.HTTPClient) { n.client = c }

// Message is what the service worker shows. Kept generic for the lock screen.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Tag   string `json:"tag"` // alert id: replaces an older notification for the same alert
	URL   string `json:"url"`
}

// MessageFor builds the lock-screen text for an alert kind in the caregiver's
// language. It never includes names or readings: anyone can see a lock screen.
func MessageFor(kind, alertID, lang string) Message {
	ar := lang != "en"
	body := "Medication alert — open Birrcare"
	if ar {
		body = "تنبيه دواء — افتح رعاية بر"
	}
	if kind == "correction" {
		body = "Correction to an earlier alert — open Birrcare"
		if ar {
			body = "تصحيح لتنبيه سابق — افتح رعاية بر"
		}
	}
	if kind == "alertness" || kind == "fast_positive" {
		body = "Health alert — open Birrcare"
		if ar {
			body = "تنبيه صحي — افتح رعاية بر"
		}
	}
	if kind == "spo2_low" {
		body = "Oxygen level alert — open Birrcare"
		if ar {
			body = "تنبيه نسبة الأكسجين — افتح رعاية بر"
		}
	}
	if kind == "reposition_overdue" {
		body = "Repositioning overdue — open Birrcare"
		if ar {
			body = "تأخر تقليب المريض — افتح رعاية بر"
		}
	}
	if kind == "bowel_overdue" {
		body = "Bowel movement overdue — open Birrcare"
		if ar {
			body = "تأخر إخراج المريض — افتح رعاية بر"
		}
	}
	if kind == "low_fluid" {
		body = "Fluid intake low today — open Birrcare"
		if ar {
			body = "سوائل منخفضة اليوم — افتح رعاية بر"
		}
	}
	if kind == "bleeding" {
		body = "Bleeding sign reported — open Birrcare"
		if ar {
			body = "تم الإبلاغ عن علامة نزيف — افتح رعاية بر"
		}
	}
	if kind == "sugar_low" || kind == "sugar_very_high" {
		body = "Blood sugar alert — open Birrcare"
		if ar {
			body = "تنبيه سكر الدم — افتح رعاية بر"
		}
	}
	if kind == "fever" || kind == "fever_high" {
		body = "Temperature alert — open Birrcare"
		if ar {
			body = "تنبيه الحرارة — افتح رعاية بر"
		}
	}
	if kind == "bp_crisis" || kind == "bp_low" {
		body = "Blood pressure alert — open Birrcare"
		if ar {
			body = "تنبيه ضغط الدم — افتح رعاية بر"
		}
	}
	if kind == "inr_out_of_range" {
		body = "An INR result is outside the doctor's range — open Birrcare"
		if ar {
			body = "نتيجة INR خارج حدود الطبيب — افتح رعاية بر"
		}
	}
	if kind == "refill_followup" {
		body = "A medicine still needs buying — open Birrcare"
		if ar {
			body = "دواء لم يُشترَ بعد — افتح رعاية بر"
		}
	}
	if kind == "appointment_reminder" {
		body = "An appointment is coming up — open Birrcare"
		if ar {
			body = "موعد قادم — افتح رعاية بر"
		}
	}
	if kind == "lab_test_due" {
		body = "A blood test is due — open Birrcare"
		if ar {
			body = "موعد تحليل دم — افتح رعاية بر"
		}
	}
	if kind == "fall" {
		body = "A fall was recorded — open Birrcare"
		if ar {
			body = "تم تسجيل سقوط — افتح رعاية بر"
		}
	}
	if kind == "seizure" {
		body = "A seizure was recorded — open Birrcare"
		if ar {
			body = "تم تسجيل نوبة تشنج — افتح رعاية بر"
		}
	}
	if kind == "dose_overdue" {
		body = "A medicine is overdue — open Birrcare"
		if ar {
			body = "دواء تأخر موعده — افتح رعاية بر"
		}
	}
	if kind == "low_stock" {
		body = "A medicine is running low — open Birrcare"
		if ar {
			body = "دواء قارب على النفاد — افتح رعاية بر"
		}
	}
	if kind == "emergency_card" {
		body = "The emergency card was opened — open Birrcare"
		if ar {
			body = "فُتحت بطاقة الطوارئ — افتح رعاية بر"
		}
	}
	if kind == "duty_handover" {
		body = "Duty was passed to you — open Birrcare to accept"
		if ar {
			body = "انتقلت إليك المناوبة — افتح رعاية بر للقبول"
		}
	}
	if kind == "duty_unaccepted" {
		body = "Your duty handover is not accepted yet — open Birrcare"
		if ar {
			body = "لم تُقبل المناوبة التي سلّمتها بعد — افتح رعاية بر"
		}
	}
	if kind == "test" {
		body = "Test alert — alerts work on this phone"
		if ar {
			body = "تنبيه تجريبي — التنبيهات تعمل على هذا الهاتف"
		}
	}
	title := "Birrcare"
	if ar {
		title = "رعاية بر"
	}
	return Message{Title: title, Body: body, Tag: alertID, URL: "/"}
}

// NotifyAsync pushes an alert in the background after the triggering write
// committed. Failures are logged; the in-app alert is the source of truth.
func (n *Notifier) NotifyAsync(alertID, kind, excludeCaregiverID string) {
	if n == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := n.Notify(ctx, alertID, kind, excludeCaregiverID); err != nil {
			log.Printf("[PUSH] alert %s: %v", alertID, err)
		}
	}()
}

type subscription struct {
	id, endpoint, p256dh, auth, lang string
}

// Notify sends the alert to every active caregiver's subscriptions except the
// excluded one. Subscriptions the push service reports gone (404/410) are removed.
func (n *Notifier) Notify(ctx context.Context, alertID, kind, excludeCaregiverID string) error {
	if n == nil {
		return nil
	}
	return n.send(ctx, alertID, kind, `c.is_active = 1 AND s.caregiver_id <> ?`, excludeCaregiverID)
}

// NotifyCaregiverAsync pushes an alert to one caregiver's phones only.
func (n *Notifier) NotifyCaregiverAsync(alertID, kind, caregiverID string) {
	if n == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := n.send(ctx, alertID, kind, `c.is_active = 1 AND s.caregiver_id = ?`, caregiverID); err != nil {
			log.Printf("[PUSH] alert %s: %v", alertID, err)
		}
	}()
}

// TestAsync sends a harmless test notification to the caregiver's own phones after
// a delay, so they can lock the phone first. No alert or clinical data is created.
func (n *Notifier) TestAsync(caregiverID string, delay time.Duration) {
	if n == nil {
		return
	}
	go func() {
		time.Sleep(delay)
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := n.send(ctx, "birrcare-test", "test", `s.caregiver_id = ?`, caregiverID); err != nil {
			log.Printf("[PUSH] test for %s: %v", caregiverID, err)
		}
	}()
}

func (n *Notifier) send(ctx context.Context, alertID, kind, where string, arg string) error {
	rows, err := n.DB.QueryContext(ctx, `
		SELECT s.id, s.endpoint, s.p256dh, s.auth, COALESCE(c.preferred_lang, 'ar')
		FROM push_subscriptions s JOIN caregivers c ON c.id = s.caregiver_id
		WHERE `+where, arg)
	if err != nil {
		return err
	}
	var subs []subscription
	for rows.Next() {
		var s subscription
		if err := rows.Scan(&s.id, &s.endpoint, &s.p256dh, &s.auth, &s.lang); err == nil {
			subs = append(subs, s)
		}
	}
	rows.Close() // release the single pooled connection before sending
	var failed int
	for _, s := range subs {
		payload, _ := json.Marshal(MessageFor(kind, alertID, s.lang))
		resp, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
			Endpoint: s.endpoint,
			Keys:     webpush.Keys{P256dh: s.p256dh, Auth: s.auth},
		}, &webpush.Options{
			HTTPClient:      n.client,
			Subscriber:      n.subscriber,
			VAPIDPublicKey:  n.PublicKey,
			VAPIDPrivateKey: n.privateKey,
			TTL:             3600,
			Urgency:         webpush.UrgencyHigh,
		})
		if err != nil {
			failed++
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		resp.Body.Close()
		switch {
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
			_, _ = n.DB.ExecContext(ctx, `DELETE FROM push_subscriptions WHERE id = ?`, s.id)
		case resp.StatusCode >= 300:
			failed++
			log.Printf("[PUSH] %s rejected: %d %s", s.endpoint, resp.StatusCode, bytes.TrimSpace(body))
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d of %d pushes failed", failed, len(subs))
	}
	return nil
}
