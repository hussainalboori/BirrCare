package logging

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"birrcare/backend/internal/auth"
)

type statusRecorder struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int64
}

func (r *statusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.statusCode == 0 {
		r.statusCode = http.StatusOK
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytesWritten += int64(n)
	return n, err
}

func (r *statusRecorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// HTTPMiddleware returns a Chi/net-http compatible middleware that automatically captures
// user requests, status codes, and errors into the dedicated user log store.
func HTTPMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if logger == nil {
				next.ServeHTTP(w, r)
				return
			}

			// Skip logging high-frequency SSE event stream and health checks to prevent log bloat
			path := r.URL.Path
			if path == "/health" || path == "/api/v1/health" || strings.HasSuffix(path, "/events/stream") {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}

			next.ServeHTTP(rec, r)

			duration := time.Since(start)
			caregiver := auth.GetCaregiverFromContext(r.Context())

			var cgID, cgName string
			if caregiver != nil {
				cgID = caregiver.ID
				cgName = caregiver.Name
			}

			level := LevelInfo
			if rec.statusCode >= 500 {
				level = LevelError
			} else if rec.statusCode >= 400 {
				level = LevelWarn
			}

			action := fmt.Sprintf("%s %s", r.Method, path)
			msg := fmt.Sprintf("HTTP %d %s %s (%s)", rec.statusCode, r.Method, path, duration)

			ip := r.Header.Get("X-Forwarded-For")
			if ip == "" {
				ip = r.RemoteAddr
			} else {
				ip = strings.Split(ip, ",")[0]
			}
			ip = strings.TrimSpace(ip)

			entry := UserLogEntry{
				CaregiverID:   cgID,
				CaregiverName: cgName,
				Level:         level,
				Action:        action,
				Resource:      path,
				StatusCode:    rec.statusCode,
				IPAddress:     ip,
				UserAgent:     r.UserAgent(),
				Message:       msg,
				Metadata: map[string]any{
					"method":          r.Method,
					"path":            path,
					"duration_ms":     duration.Milliseconds(),
					"bytes_written":   rec.bytesWritten,
					"query":           r.URL.RawQuery,
				},
				CreatedAt: start.UTC(),
			}

			// Fire synchronously into user log store
			if rec.statusCode >= 400 {
				_ = logger.LogUserError(r.Context(), entry)
			} else {
				_ = logger.LogUserActivity(r.Context(), entry)
			}
		})
	}
}
