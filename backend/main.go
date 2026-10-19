package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"birrcare/backend/internal/auth"
	"birrcare/backend/internal/db"
	"birrcare/backend/internal/handlers"
	"birrcare/backend/internal/sse"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/birrcare.db"
	}

	log.Printf("Starting Birrcare backend on :%s (Database: %s)", port, dbPath)

	// 1. Initialize SQLite connection pool in WAL mode
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Database initialization failed: %v", err)
	}
	defer database.Close()

	// 2. Run migrations and seed baseline data
	if err := db.Migrate(database); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	// 3. Initialize SSE Broker
	broker := sse.InitBroker()

	// 4. Handlers
	authHandler := &handlers.AuthHandler{DB: database, Broker: broker}
	usersHandler := &handlers.UsersHandler{DB: database, Broker: broker}
	medHandler := &handlers.MedicationHandler{DB: database, Broker: broker}
	vitalsHandler := &handlers.VitalsHandler{DB: database, Broker: broker}
	handoverHandler := &handlers.HandoverHandler{DB: database, Broker: broker}
	exportHandler := &handlers.ExportHandler{DB: database}

	// 5. Router & Middleware
	r := chi.NewRouter()

	r.Use(chimw.RequestID)
	r.Use(chimw.RealIP)
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.Timeout(60 * time.Second))

	// CORS configuration for local dev and decoupled Next.js container
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000", "http://127.0.0.1:3000"},
		AllowOriginFunc:  func(r *http.Request, origin string) bool { return true },
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token", "Cf-Access-Authenticated-User-Email"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Apply authentication middleware
	r.Use(auth.Middleware(database))

	// Health check
	healthHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"healthy","service":"birrcare-backend"}`))
	}
	r.Get("/health", healthHandler)

	// API v1 Routes
	r.Route("/api/v1", func(api chi.Router) {
		api.Get("/health", healthHandler)

		// Real-time Event Stream
		api.Get("/events/stream", broker.ServeHTTP)

		// Caregivers & Auth
		api.Get("/caregivers", authHandler.ListCaregivers)
		api.Get("/auth/me", authHandler.GetMe)
		api.Post("/auth/verify-pin", authHandler.VerifyPIN)
		api.Post("/auth/logout", authHandler.Logout)

		// User Management (Super Admin only)
		api.Get("/users", usersHandler.ListUsers)
		api.Post("/users", usersHandler.CreateUser)
		api.Put("/users/{id}", usersHandler.UpdateUser)
		api.Post("/users/{id}/reset-pin", usersHandler.ResetUserPIN)
		api.Delete("/users/{id}", usersHandler.ToggleUserActive)

		// Medications & Scheduling
		api.Get("/medications", medHandler.ListMedications)
		api.Post("/medications", medHandler.CreateMedication)
		api.Put("/medications/{id}", medHandler.UpdateMedication)
		api.Delete("/medications/{id}", medHandler.DeleteMedication)
		api.Get("/medications/schedule", medHandler.GetSchedule)
		api.Post("/medications/log", medHandler.LogDose)

		// Stroke Vitals
		api.Get("/vitals", vitalsHandler.ListVitals)
		api.Post("/vitals", vitalsHandler.LogVitals)

		// Handover Notes
		api.Get("/handovers", handoverHandler.ListHandovers)
		api.Post("/handovers", handoverHandler.CreateHandover)

		// Doctor Consultation Summary
		api.Get("/export/summary", exportHandler.ExportDoctorSummary)
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 0, // Infinite for SSE text/event-stream
		IdleTimeout:  60 * time.Second,
	}

	// Server run loop
	go func() {
		log.Printf("Birrcare API listening on http://0.0.0.0:%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server ListenAndServe error: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down Birrcare backend...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	log.Println("Birrcare backend stopped gracefully.")
}
