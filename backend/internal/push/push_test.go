package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"birrcare/backend/internal/db"
)

func testDB(t *testing.T) *Notifier {
	t.Helper()
	database, err := db.InitDB(t.TempDir() + "/push.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := db.Migrate(database); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO caregivers (id, name, role, pin_hash, preferred_lang) VALUES
		('cg_a', 'Fatima Secret', 'caregiver', 'x', 'en'), ('cg_b', 'Ali', 'caregiver', 'x', 'ar'), ('cg_off', 'Gone', 'caregiver', 'x', 'en')`); err != nil {
		t.Fatal(err)
	}
	database.Exec(`UPDATE caregivers SET is_active = 0 WHERE id = 'cg_off'`)
	n, err := Init(database)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func subKeys(t *testing.T) (string, string) {
	t.Helper()
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	rand.Read(auth)
	return base64.RawURLEncoding.EncodeToString(k.PublicKey().Bytes()), base64.RawURLEncoding.EncodeToString(auth)
}

func TestVAPIDKeysPersistAcrossRestarts(t *testing.T) {
	n := testDB(t)
	again, err := Init(n.DB)
	if err != nil {
		t.Fatal(err)
	}
	if n.PublicKey == "" || again.PublicKey != n.PublicKey || again.privateKey != n.privateKey {
		t.Fatal("VAPID keys must be generated once and reused, or every phone's subscription breaks")
	}
}

// Lock-screen privacy: no names, no readings, only a generic line per kind.
func TestMessageIsGenericOnTheLockScreen(t *testing.T) {
	cases := map[string]map[string]string{
		"bp_crisis":     {"en": "Blood pressure alert — open Birrcare", "ar": "تنبيه ضغط الدم — افتح رعاية بر"},
		"dose_override": {"en": "Medication alert — open Birrcare", "ar": "تنبيه دواء — افتح رعاية بر"},
		"vomited":       {"en": "Medication alert — open Birrcare", "ar": "تنبيه دواء — افتح رعاية بر"},
		"bp_low":        {"en": "Blood pressure alert — open Birrcare", "ar": "تنبيه ضغط الدم — افتح رعاية بر"},
		"alertness":     {"en": "Health alert — open Birrcare", "ar": "تنبيه صحي — افتح رعاية بر"},
		"fast_positive": {"en": "Health alert — open Birrcare", "ar": "تنبيه صحي — افتح رعاية بر"},
		"bleeding":      {"en": "Bleeding sign reported — open Birrcare", "ar": "تم الإبلاغ عن علامة نزيف — افتح رعاية بر"},
	}
	for kind, langs := range cases {
		for lang, body := range langs {
			m := MessageFor(kind, "alert-1", lang)
			if m.Body != body || m.Tag != "alert-1" || m.URL != "/" {
				t.Fatalf("%s/%s: %+v", kind, lang, m)
			}
			if strings.ContainsAny(m.Title+m.Body, "0123456789") {
				t.Fatalf("%s/%s: lock-screen text must carry no values: %+v", kind, lang, m)
			}
		}
	}
}

func TestNotifySendsToOthersAndDropsExpiredSubscriptions(t *testing.T) {
	n := testDB(t)
	var mu sync.Mutex
	got := map[string]*http.Request{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got[r.URL.Path] = r
		mu.Unlock()
		if r.URL.Path == "/gone" {
			w.WriteHeader(http.StatusGone)
			return
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	n.client = srv.Client()

	add := func(id, caregiver, path string) {
		p, a := subKeys(t)
		if _, err := n.DB.Exec(`INSERT INTO push_subscriptions (id, caregiver_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?, ?)`, id, caregiver, srv.URL+path, p, a); err != nil {
			t.Fatal(err)
		}
	}
	add("s-self", "cg_a", "/self") // the caregiver who logged it
	add("s-ali", "cg_b", "/ali")   // a sibling
	add("s-gone", "cg_b", "/gone") // a phone that uninstalled the app
	add("s-off", "cg_off", "/off") // deactivated caregiver

	if err := n.Notify(context.Background(), "alert-1", "bp_crisis", "cg_a"); err != nil {
		t.Fatal(err)
	}
	if got["/self"] != nil || got["/off"] != nil {
		t.Fatal("must not push to the caregiver who caused the alert or to deactivated caregivers")
	}
	r := got["/ali"]
	if r == nil {
		t.Fatal("sibling was not notified")
	}
	if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Authorization"), "vapid t=") ||
		r.Header.Get("Urgency") != "high" || r.Header.Get("TTL") != "3600" || r.Header.Get("Content-Encoding") != "aes128gcm" {
		t.Fatalf("push request headers: %v", r.Header)
	}
	var left int
	n.DB.QueryRow(`SELECT COUNT(*) FROM push_subscriptions WHERE id = 's-gone'`).Scan(&left)
	if left != 0 {
		t.Fatal("a subscription the push service reports gone (410) must be removed")
	}
}

func TestTestPushGoesOnlyToTheCallersPhones(t *testing.T) {
	n := testDB(t)
	var mu sync.Mutex
	hits := map[string]int{}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits[r.URL.Path]++
		mu.Unlock()
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	n.client = srv.Client()
	for _, s := range []struct{ id, cg, path string }{{"a", "cg_a", "/mine"}, {"b", "cg_b", "/other"}} {
		p, a := subKeys(t)
		n.DB.Exec(`INSERT INTO push_subscriptions (id, caregiver_id, endpoint, p256dh, auth) VALUES (?, ?, ?, ?, ?)`, s.id, s.cg, srv.URL+s.path, p, a)
	}
	if err := n.send(context.Background(), "birrcare-test", "test", `s.caregiver_id = ?`, "cg_a"); err != nil {
		t.Fatal(err)
	}
	if hits["/mine"] != 1 || hits["/other"] != 0 {
		t.Fatalf("test push must reach only the caller's phones: %v", hits)
	}
	if m := MessageFor("test", "x", "en"); m.Body != "Test alert — alerts work on this phone" {
		t.Fatalf("test message: %+v", m)
	}
	var alerts int
	n.DB.QueryRow(`SELECT COUNT(*) FROM alerts`).Scan(&alerts)
	if alerts != 0 {
		t.Fatal("a test push must not create an alert")
	}
}

func TestNilNotifierIsSafe(t *testing.T) {
	var n *Notifier
	n.NotifyAsync("a", "bp_crisis", "x")
	if err := n.Notify(context.Background(), "a", "bp_crisis", "x"); err != nil {
		t.Fatal(err)
	}
}
