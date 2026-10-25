package logging

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	timeFormatSQLite = "2006-01-02 15:04:05"
	defaultPageLimit = 50
	maxPageLimit     = 500
)

// UserLogStore defines persistence operations for user activity and user error streams.
type UserLogStore interface {
	WriteUserLog(ctx context.Context, entry UserLogEntry) error
	GetUserLogByID(ctx context.Context, id string) (*UserLogEntry, error)
	QueryUserLogs(ctx context.Context, filter LogFilter) ([]UserLogEntry, int, error)
}

// SystemLogStore defines persistence operations for system lifecycle, worker, and operational event streams.
type SystemLogStore interface {
	WriteSystemLog(ctx context.Context, entry SystemLogEntry) error
	GetSystemLogByID(ctx context.Context, id string) (*SystemLogEntry, error)
	QuerySystemLogs(ctx context.Context, filter LogFilter) ([]SystemLogEntry, int, error)
}

// AccessAuditStore defines persistence operations for auditing super admin access to logs.
type AccessAuditStore interface {
	RecordAccess(ctx context.Context, entry AccessAuditEntry) error
	QueryAccess(ctx context.Context, filter AccessAuditFilter) ([]AccessAuditEntry, int, error)
}

// SQLiteUserLogStore implements UserLogStore against the user_logs SQLite table.
type SQLiteUserLogStore struct {
	db *sql.DB
}

// NewSQLiteUserLogStore creates a new SQLiteUserLogStore.
func NewSQLiteUserLogStore(db *sql.DB) *SQLiteUserLogStore {
	return &SQLiteUserLogStore{db: db}
}

func (s *SQLiteUserLogStore) WriteUserLog(ctx context.Context, entry UserLogEntry) error {
	if s.db == nil {
		return errors.New("user log store database handle is nil")
	}
	id := entry.ID
	if id == "" {
		id = uuid.New().String()
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdStr := createdAt.UTC().Format(timeFormatSQLite)
	metaStr := MarshalMetadata(entry.Metadata)

	var cgArg any = entry.CaregiverID
	if entry.CaregiverID == "" {
		cgArg = nil
	}

	query := `
		INSERT INTO user_logs (
			id, caregiver_id, caregiver_name, level, action, resource,
			status_code, ip_address, user_agent, message, metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		id, cgArg, entry.CaregiverName, string(entry.Level), entry.Action, entry.Resource,
		entry.StatusCode, entry.IPAddress, entry.UserAgent, entry.Message, metaStr, createdStr,
	)
	if err != nil {
		return fmt.Errorf("failed inserting user log: %w", err)
	}
	return nil
}

func (s *SQLiteUserLogStore) GetUserLogByID(ctx context.Context, id string) (*UserLogEntry, error) {
	if s.db == nil {
		return nil, errors.New("user log store database handle is nil")
	}
	query := `
		SELECT id, caregiver_id, caregiver_name, level, action, resource,
		       status_code, ip_address, user_agent, message, metadata, created_at
		FROM user_logs
		WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	entry, err := scanUserLog(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed fetching user log by id %q: %w", id, err)
	}
	return entry, nil
}

func (s *SQLiteUserLogStore) QueryUserLogs(ctx context.Context, filter LogFilter) ([]UserLogEntry, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("user log store database handle is nil")
	}

	var where []string
	var args []any

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		where = append(where, "(message LIKE ? OR action LIKE ? OR resource LIKE ? OR caregiver_name LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filter.Level != "" {
		where = append(where, "level = ?")
		args = append(args, string(filter.Level))
	}
	if filter.CaregiverID != "" {
		where = append(where, "caregiver_id = ?")
		args = append(args, filter.CaregiverID)
	}
	if filter.Action != "" {
		where = append(where, "action = ?")
		args = append(args, filter.Action)
	}
	if filter.From != nil && !filter.From.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(timeFormatSQLite))
	}
	if filter.To != nil && !filter.To.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, filter.To.UTC().Format(timeFormatSQLite))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// 1. Total matching count
	countQuery := "SELECT COUNT(*) FROM user_logs " + whereClause
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed counting user logs: %w", err)
	}

	// 2. Query paginated rows
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	} else if limit > maxPageLimit {
		limit = maxPageLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := fmt.Sprintf(`
		SELECT id, caregiver_id, caregiver_name, level, action, resource,
		       status_code, ip_address, user_agent, message, metadata, created_at
		FROM user_logs
		%s
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed querying user logs: %w", err)
	}
	defer rows.Close()

	var entries []UserLogEntry
	for rows.Next() {
		e, err := scanUserLogRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed scanning user log row: %w", err)
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("user log rows iteration error: %w", err)
	}

	return entries, total, nil
}

// SQLiteSystemLogStore implements SystemLogStore against the system_logs SQLite table.
type SQLiteSystemLogStore struct {
	db *sql.DB
}

// NewSQLiteSystemLogStore creates a new SQLiteSystemLogStore.
func NewSQLiteSystemLogStore(db *sql.DB) *SQLiteSystemLogStore {
	return &SQLiteSystemLogStore{db: db}
}

func (s *SQLiteSystemLogStore) WriteSystemLog(ctx context.Context, entry SystemLogEntry) error {
	if s.db == nil {
		return errors.New("system log store database handle is nil")
	}
	id := entry.ID
	if id == "" {
		id = uuid.New().String()
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdStr := createdAt.UTC().Format(timeFormatSQLite)
	metaStr := MarshalMetadata(entry.Metadata)

	query := `
		INSERT INTO system_logs (
			id, level, subsystem, event, message, error_details, metadata, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		id, string(entry.Level), entry.Subsystem, entry.Event,
		entry.Message, entry.ErrorDetails, metaStr, createdStr,
	)
	if err != nil {
		return fmt.Errorf("failed inserting system log: %w", err)
	}
	return nil
}

func (s *SQLiteSystemLogStore) GetSystemLogByID(ctx context.Context, id string) (*SystemLogEntry, error) {
	if s.db == nil {
		return nil, errors.New("system log store database handle is nil")
	}
	query := `
		SELECT id, level, subsystem, event, message, error_details, metadata, created_at
		FROM system_logs
		WHERE id = ?
	`
	row := s.db.QueryRowContext(ctx, query, id)
	entry, err := scanSystemLog(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed fetching system log by id %q: %w", id, err)
	}
	return entry, nil
}

func (s *SQLiteSystemLogStore) QuerySystemLogs(ctx context.Context, filter LogFilter) ([]SystemLogEntry, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("system log store database handle is nil")
	}

	var where []string
	var args []any

	if filter.Query != "" {
		pattern := "%" + filter.Query + "%"
		where = append(where, "(message LIKE ? OR subsystem LIKE ? OR event LIKE ? OR error_details LIKE ?)")
		args = append(args, pattern, pattern, pattern, pattern)
	}
	if filter.Level != "" {
		where = append(where, "level = ?")
		args = append(args, string(filter.Level))
	}
	if filter.Subsystem != "" {
		where = append(where, "subsystem = ?")
		args = append(args, filter.Subsystem)
	}
	if filter.Event != "" {
		where = append(where, "event = ?")
		args = append(args, filter.Event)
	}
	if filter.From != nil && !filter.From.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(timeFormatSQLite))
	}
	if filter.To != nil && !filter.To.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, filter.To.UTC().Format(timeFormatSQLite))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// 1. Total matching count
	countQuery := "SELECT COUNT(*) FROM system_logs " + whereClause
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed counting system logs: %w", err)
	}

	// 2. Query paginated rows
	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	} else if limit > maxPageLimit {
		limit = maxPageLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := fmt.Sprintf(`
		SELECT id, level, subsystem, event, message, error_details, metadata, created_at
		FROM system_logs
		%s
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed querying system logs: %w", err)
	}
	defer rows.Close()

	var entries []SystemLogEntry
	for rows.Next() {
		e, err := scanSystemLogRow(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("failed scanning system log row: %w", err)
		}
		entries = append(entries, *e)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("system log rows iteration error: %w", err)
	}

	return entries, total, nil
}

// SQLiteAccessAuditStore implements AccessAuditStore against the log_access_audits SQLite table.
type SQLiteAccessAuditStore struct {
	db *sql.DB
}

// NewSQLiteAccessAuditStore creates a new SQLiteAccessAuditStore.
func NewSQLiteAccessAuditStore(db *sql.DB) *SQLiteAccessAuditStore {
	return &SQLiteAccessAuditStore{db: db}
}

func (s *SQLiteAccessAuditStore) RecordAccess(ctx context.Context, entry AccessAuditEntry) error {
	if s.db == nil {
		return errors.New("access audit store database handle is nil")
	}
	id := entry.ID
	if id == "" {
		id = uuid.New().String()
	}
	createdAt := entry.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	createdStr := createdAt.UTC().Format(timeFormatSQLite)

	query := `
		INSERT INTO log_access_audits (
			id, caregiver_id, caregiver_name, stream, operation,
			filter_query, records_count, ip_address, user_agent, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := s.db.ExecContext(ctx, query,
		id, entry.CaregiverID, entry.CaregiverName, entry.Stream, entry.Operation,
		entry.FilterQuery, entry.RecordsCount, entry.IPAddress, entry.UserAgent, createdStr,
	)
	if err != nil {
		return fmt.Errorf("failed recording log access audit: %w", err)
	}
	return nil
}

func (s *SQLiteAccessAuditStore) QueryAccess(ctx context.Context, filter AccessAuditFilter) ([]AccessAuditEntry, int, error) {
	if s.db == nil {
		return nil, 0, errors.New("access audit store database handle is nil")
	}

	var where []string
	var args []any

	if filter.CaregiverID != "" {
		where = append(where, "caregiver_id = ?")
		args = append(args, filter.CaregiverID)
	}
	if filter.Stream != "" {
		where = append(where, "stream = ?")
		args = append(args, filter.Stream)
	}
	if filter.Operation != "" {
		where = append(where, "operation = ?")
		args = append(args, filter.Operation)
	}
	if filter.From != nil && !filter.From.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, filter.From.UTC().Format(timeFormatSQLite))
	}
	if filter.To != nil && !filter.To.IsZero() {
		where = append(where, "created_at <= ?")
		args = append(args, filter.To.UTC().Format(timeFormatSQLite))
	}

	whereClause := ""
	if len(where) > 0 {
		whereClause = "WHERE " + strings.Join(where, " AND ")
	}

	// Count
	countQuery := "SELECT COUNT(*) FROM log_access_audits " + whereClause
	var total int
	if err := s.db.QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("failed counting log access audits: %w", err)
	}

	limit := filter.Limit
	if limit <= 0 {
		limit = defaultPageLimit
	} else if limit > maxPageLimit {
		limit = maxPageLimit
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}

	selectQuery := fmt.Sprintf(`
		SELECT id, caregiver_id, caregiver_name, stream, operation,
		       filter_query, records_count, ip_address, user_agent, created_at
		FROM log_access_audits
		%s
		ORDER BY created_at DESC, rowid DESC
		LIMIT ? OFFSET ?
	`, whereClause)

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, selectQuery, queryArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed querying log access audits: %w", err)
	}
	defer rows.Close()

	var entries []AccessAuditEntry
	for rows.Next() {
		var a AccessAuditEntry
		var filterQuery, ip, ua sql.NullString
		var createdStr string

		err := rows.Scan(
			&a.ID, &a.CaregiverID, &a.CaregiverName, &a.Stream, &a.Operation,
			&filterQuery, &a.RecordsCount, &ip, &ua, &createdStr,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed scanning access audit row: %w", err)
		}
		if filterQuery.Valid {
			a.FilterQuery = filterQuery.String
		}
		if ip.Valid {
			a.IPAddress = ip.String
		}
		if ua.Valid {
			a.UserAgent = ua.String
		}
		a.CreatedAt, _ = time.Parse(timeFormatSQLite, createdStr)
		entries = append(entries, a)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("access audit rows iteration error: %w", err)
	}

	return entries, total, nil
}

// Scanner abstraction for scanning sql.Row and sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanUserLog(s scanner) (*UserLogEntry, error) {
	var e UserLogEntry
	var cgID, cgName, resource, ip, ua, meta sql.NullString
	var status sql.NullInt64
	var levelStr, createdStr string

	err := s.Scan(
		&e.ID, &cgID, &cgName, &levelStr, &e.Action, &resource,
		&status, &ip, &ua, &e.Message, &meta, &createdStr,
	)
	if err != nil {
		return nil, err
	}
	e.Level = LogLevel(levelStr)
	if cgID.Valid {
		e.CaregiverID = cgID.String
	}
	if cgName.Valid {
		e.CaregiverName = cgName.String
	}
	if resource.Valid {
		e.Resource = resource.String
	}
	if status.Valid {
		e.StatusCode = int(status.Int64)
	}
	if ip.Valid {
		e.IPAddress = ip.String
	}
	if ua.Valid {
		e.UserAgent = ua.String
	}
	if meta.Valid {
		e.Metadata = UnmarshalMetadata(meta.String)
	}
	e.CreatedAt, _ = time.Parse(timeFormatSQLite, createdStr)
	return &e, nil
}

func scanUserLogRow(rows *sql.Rows) (*UserLogEntry, error) {
	return scanUserLog(rows)
}

func scanSystemLog(s scanner) (*SystemLogEntry, error) {
	var e SystemLogEntry
	var errDetails, meta sql.NullString
	var levelStr, createdStr string

	err := s.Scan(
		&e.ID, &levelStr, &e.Subsystem, &e.Event,
		&e.Message, &errDetails, &meta, &createdStr,
	)
	if err != nil {
		return nil, err
	}
	e.Level = LogLevel(levelStr)
	if errDetails.Valid {
		e.ErrorDetails = errDetails.String
	}
	if meta.Valid {
		e.Metadata = UnmarshalMetadata(meta.String)
	}
	e.CreatedAt, _ = time.Parse(timeFormatSQLite, createdStr)
	return &e, nil
}

func scanSystemLogRow(rows *sql.Rows) (*SystemLogEntry, error) {
	return scanSystemLog(rows)
}
