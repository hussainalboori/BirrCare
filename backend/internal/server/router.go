package server

import (
	"database/sql"
	"net/http"
	"os"
	"strings"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/sse"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

// Options configures the HTTP router.
type Options struct {
	DB             *sql.DB
	Broker         *sse.Broker
	Auth           auth.Config
	Limiter        *auth.PINLimiter
	RequestTimeout time.Duration // per-request timeout for non-streaming routes
	MaxBodyBytes   int64
	AllowedOrigins []string
	Contacts       handlers.Contacts
	Push           *push.Notifier // nil = no Web Push
	// EmergencyLimiter rate-limits the public emergency card (nil = 10/min per IP).
	EmergencyLimiter *handlers.IPRateLimiter
}

// OptionsFromEnv returns production defaults, reading CORS_ALLOWED_ORIGINS
// (comma separated) and the auth.ConfigFromEnv settings.
func OptionsFromEnv(database *sql.DB, broker *sse.Broker) Options {
	origins := []string{"http://localhost:3000", "http://127.0.0.1:3000"}
	if v := os.Getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		origins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
	}
	return Options{
		DB:             database,
		Broker:         broker,
		Auth:           auth.ConfigFromEnv(),
		Limiter:        auth.NewPINLimiter(),
		RequestTimeout: 60 * time.Second,
		MaxBodyBytes:   1 << 20,
		AllowedOrigins: origins,
		Contacts:       handlers.ContactsFromEnv(),
	}
}

func limitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && n > 0 {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// NewRouter wires middleware and routes.
func NewRouter(o Options) http.Handler {
	authHandler := &handlers.AuthHandler{DB: o.DB, Broker: o.Broker, Limiter: o.Limiter}
	usersHandler := &handlers.UsersHandler{DB: o.DB, Broker: o.Broker}
	medHandler := &handlers.MedicationHandler{DB: o.DB, Broker: o.Broker, Push: o.Push}
	vitalsHandler := &handlers.VitalsHandler{DB: o.DB, Broker: o.Broker, Push: o.Push}
	alertsHandler := &handlers.AlertsHandler{DB: o.DB, Push: o.Push}
	clinicalHandler := &handlers.ClinicalSettingsHandler{DB: o.DB}
	handlers.LoadClinicalSettings(o.DB)
	handoverHandler := &handlers.HandoverHandler{DB: o.DB, Broker: o.Broker}
	exportHandler := &handlers.ExportHandler{DB: o.DB}
	shiftsHandler := &handlers.ShiftsHandler{DB: o.DB, Broker: o.Broker, Push: o.Push}
	contactsHandler := &handlers.ContactsHandler{Contacts: o.Contacts}
	emergencyHandler := &handlers.EmergencyHandler{DB: o.DB, Broker: o.Broker, Push: o.Push, Contacts: o.Contacts, Limiter: o.EmergencyLimiter}
	insulinHandler := &handlers.InsulinHandler{DB: o.DB, Broker: o.Broker}

	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(limitBody(o.MaxBodyBytes))
	r.Use(securityHeaders)

	// CORS only matters for local dev (prod is same-origin through the Next.js proxy).
	// Never reflect arbitrary origins with credentials.
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   o.AllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Use(auth.MiddlewareWithConfig(o.DB, o.Auth))

	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","service":"birrcare-backend"}`))
	}
	r.Get("/health", healthHandler)

	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", healthHandler)

		// Real-time event stream: authenticated, but deliberately OUTSIDE the request
		// timeout - chimw.Timeout would cancel r.Context() and kill every stream at 60s.
		api.With(auth.RequireAuth).Get("/events/stream", o.Broker.ServeHTTP)

		api.Group(func(api chi.Router) {
			if o.RequestTimeout > 0 {
				api.Use(chimw.Timeout(o.RequestTimeout))
			}

			// Public: needed to render the PIN switcher before anyone is signed in.
			api.Get("/caregivers", authHandler.ListCaregivers)
			api.Post("/auth/verify-pin", authHandler.VerifyPIN)
			api.Post("/auth/logout", authHandler.Logout)
			// Second login step after a PIN for accounts with an authenticator app.
			api.Post("/auth/verify-2fa", authHandler.VerifyTwoFactor)
			// Public: a crisis screen must show the doctor's number even if the PIN
			// session has expired. The whole site is already behind Cloudflare Access.
			api.Get("/contacts", contactsHandler.GetContacts)
			// Public (issue #19): the paramedics' card. The token is the secret;
			// Cloudflare Access bypasses /emergency/* only.
			api.Get("/emergency/{token}", emergencyHandler.Card)

			api.Group(func(api chi.Router) {
				api.Use(auth.RequireAuth)

				api.Get("/auth/me", authHandler.GetMe)
				api.Post("/auth/change-pin", authHandler.ChangePIN)
				api.Post("/auth/sign-out-others", authHandler.SignOutOtherDevices)
				api.Post("/auth/touch", authHandler.Touch)
				api.Post("/auth/device-mode", authHandler.SetDeviceMode)
				api.Post("/auth/2fa/setup", authHandler.SetupTwoFactor)
				api.Post("/auth/2fa/enable", authHandler.EnableTwoFactor)
				api.Post("/auth/2fa/disable", authHandler.DisableTwoFactor)

				// User Management (Super Admin only - enforced in handlers)
				api.Get("/users", usersHandler.ListUsers)
				api.Post("/users", usersHandler.CreateUser)
				api.Put("/users/{id}", usersHandler.UpdateUser)
				api.Post("/users/{id}/reset-pin", usersHandler.ResetUserPIN)
				api.Delete("/users/{id}", usersHandler.ToggleUserActive)

				// Medications & Scheduling
				api.Get("/medications", medHandler.ListMedications)
				api.Post("/medications", medHandler.CreateMedication)
				api.Put("/medications/{id}", medHandler.UpdateMedication)
				api.Put("/medications/{id}/schedule", medHandler.UpdateSchedule)
				api.Delete("/medications/{id}", medHandler.DeleteMedication)
				api.Get("/medications/schedule", medHandler.GetSchedule)
				api.Post("/medications/log", medHandler.LogDose)
				api.Get("/medications/stock", medHandler.ListStock)
				api.Post("/medications/{id}/stock", medHandler.UpdateStock)
				api.Post("/medications/log/{id}/void", medHandler.VoidDose)
				api.Post("/medications/log/{id}/event", medHandler.AddDoseEvent)

				// PRN (As-Needed) Medications (Issue #25)
				api.Get("/medications/prn", medHandler.GetPRNMedications)
				api.Post("/medications/prn/log", medHandler.LogPRNDose)
				api.Post("/medications/prn/log/{id}/void", medHandler.VoidPRNDose)

				// Alerts that must reach every sibling (issue #12)
				api.Get("/alerts/unacked", alertsHandler.Unacked)
				api.Post("/alerts/{id}/ack", alertsHandler.Ack)
				api.Get("/push/key", alertsHandler.PushKey)
				api.Post("/push/subscribe", alertsHandler.Subscribe)
				api.Post("/push/unsubscribe", alertsHandler.Unsubscribe)
				api.Post("/push/test", alertsHandler.TestPush)

				// Clinical thresholds and the F.A.S.T. check (issue #13)
				api.Get("/settings/clinical", clinicalHandler.Get)
				api.Put("/settings/clinical", clinicalHandler.Put)
				api.Put("/vitals/{id}/last-normal", vitalsHandler.SetLastNormal)
				api.Post("/vitals/{id}/correct", vitalsHandler.CorrectVitals)

				// Stroke Vitals
				api.Get("/vitals", vitalsHandler.ListVitals)
				api.Post("/vitals", vitalsHandler.LogVitals)

				// Insulin: doctor's correction scale / fixed dose, and injections
				api.Get("/insulin/plans", insulinHandler.ListPlans)
				api.Post("/insulin/plans", insulinHandler.CreatePlan)
				api.Put("/insulin/plans/{id}", insulinHandler.UpdatePlan)
				api.Get("/insulin/status", insulinHandler.GetStatus)
				api.Post("/insulin/log", insulinHandler.LogInsulin)

				// Handover Notes
				api.Get("/handovers", handoverHandler.ListHandovers)
				api.Post("/handovers", handoverHandler.CreateHandover)

				// Doctor Consultation Summary
				api.Get("/export/summary", exportHandler.ExportDoctorSummary)

				// Caregiver Work Shifts & Duty Management (Super Admin controls, all can read)
				api.Get("/shifts", shiftsHandler.ListShifts)
				api.Post("/shifts", shiftsHandler.CreateShift)
				api.Put("/shifts/{id}", shiftsHandler.UpdateShift)
				api.Delete("/shifts/{id}", shiftsHandler.DeleteShift)
				api.Post("/shifts/presets", shiftsHandler.ApplyPreset)
				api.Get("/shifts/assignments", shiftsHandler.ListAssignments)
				api.Post("/shifts/assignments", shiftsHandler.AssignShift)
				api.Delete("/shifts/assignments/{id}", shiftsHandler.DeleteAssignment)
				api.Get("/shifts/current-duty", shiftsHandler.GetCurrentDuty)
				api.Post("/shifts/handover", shiftsHandler.PassDuty)
				api.Get("/patient", emergencyHandler.GetProfile)
				api.Put("/patient", emergencyHandler.UpdateProfile)
				api.Get("/patient/emergency-link", emergencyHandler.EmergencyLink)
				api.Post("/patient/emergency-link", emergencyHandler.RotateLink)
				api.Post("/shifts/handover/{id}/accept", shiftsHandler.AcceptDuty)
			})
		})
	})

	return r
}

// securityHeaders hardens every API response: JSON is never sniffed as HTML,
// never framed, and health data is never kept in browser or proxy caches.
// Handlers may override Cache-Control (the SSE stream sets no-cache).
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}
