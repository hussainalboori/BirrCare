// Package logging provides a production-ready, centralized logging and auditing
// system for Birrcare.
//
// The package enforces a strict architectural separation of concerns between two
// distinct event streams:
//  1. User activity and errors (UserLogStore): records actions and errors
//     performed by ordinary caregivers and users during normal application usage.
//  2. System events (SystemLogStore): records infrastructure, database, server,
//     background worker, and subsystem lifecycle events.
//
// Both streams are captured through a single, centralized Logger instance that is safe
// for concurrent access and guarantees explicit error handling if any storage sink fails.
//
// Access to either log stream is strictly restricted to Super Admins. Every access
// (read, list, search, or export) is recorded in a tamper-evident access audit log
// (AccessAuditStore) that is itself protected from unauthorized viewing.
package logging
