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
	"birrcare/backend/internal/push"
	"birrcare/backend/internal/server"
	"birrcare/backend/internal/sse"
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
	log.Printf("Household clinical time zone: %s", handlers.ClinicalLocation())

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

	// 3. Session signing key (SESSION_SECRET env, else persisted random key)
	if err := auth.InitSessionSecret(database); err != nil {
		log.Fatalf("Session secret initialization failed: %v", err)
	}

	// 4. Initialize SSE Broker
	broker := sse.InitBroker()
	defer broker.Close()

	// 5. Web Push keys (generated once and stored, like the session secret)
	notifier, err := push.Init(database)
	if err != nil {
		log.Printf("[PUSH] disabled: %v", err)
	}

	// 6. Router & Middleware
	opts := server.OptionsFromEnv(database, broker)
	opts.Push = notifier
	opts.UpdateDir = handlers.UpdateDirFromEnv(dbPath)
	r := server.NewRouter(opts)

	// 7. Tell a sibling when the duty they passed has not been accepted (issue #18)
	reminderCtx, stopReminders := context.WithCancel(context.Background())
	defer stopReminders()
	handlers.StartDutyReminders(reminderCtx, database, notifier, broker)
	handlers.StartUpdateWatcher(reminderCtx, opts.UpdateDir, broker)

	// 8. Watch for overdue patient repositioning to prevent pressure sores (issue #27)
	handlers.StartRepositionWatcher(reminderCtx, database, notifier, broker)

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%s", port),
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      0, // Infinite for SSE text/event-stream; non-stream routes use chimw.Timeout
		IdleTimeout:       60 * time.Second,
	}

	// Shutdown waits for in-flight requests; SSE streams never end on their own,
	// so close the broker first to make every stream handler return.
	srv.RegisterOnShutdown(broker.Close)

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
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Birrcare backend stopped gracefully.")
}
