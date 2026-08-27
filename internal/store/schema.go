package store

import (
	"context"
	"fmt"
)

const schemaVersion = 1

// migrate 建立持久化快照之外的规范化索引、不可变审计记录和幂等结果表。
func (s *Store) migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys=ON`,
		`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`,
		`INSERT INTO schema_version(version) SELECT 1 WHERE NOT EXISTS (SELECT 1 FROM schema_version)`,
		`CREATE TABLE IF NOT EXISTS productions (id TEXT PRIMARY KEY, revision INTEGER NOT NULL CHECK(revision>0), state TEXT NOT NULL, snapshot_json BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS rigging_elements (production_id TEXT NOT NULL, element_id TEXT NOT NULL, revision INTEGER NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,element_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS motion_cues (production_id TEXT NOT NULL, cue_id TEXT NOT NULL, sequence_no INTEGER NOT NULL, revision INTEGER NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,cue_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS analysis_findings (production_id TEXT NOT NULL, finding_id TEXT NOT NULL, analysis_revision INTEGER NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,finding_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS analysis_batches (production_id TEXT NOT NULL, analysis_revision INTEGER NOT NULL, findings_json BLOB NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(production_id,analysis_revision), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS rehearsal_evidence (production_id TEXT NOT NULL, evidence_id TEXT NOT NULL, cue_id TEXT NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,evidence_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS review_decisions (production_id TEXT NOT NULL, review_id TEXT NOT NULL, payload_json BLOB NOT NULL, decided_at TEXT NOT NULL, PRIMARY KEY(production_id,review_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS frozen_manifests (production_id TEXT PRIMARY KEY, revision INTEGER NOT NULL, sha256 TEXT NOT NULL, canonical_json BLOB NOT NULL, FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS release_credentials (production_id TEXT NOT NULL, credential_id TEXT NOT NULL, code TEXT NOT NULL UNIQUE, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,credential_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS finding_remediations (production_id TEXT NOT NULL, remediation_id TEXT NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,remediation_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS review_response_items (production_id TEXT NOT NULL, item_id TEXT NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,item_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS credential_status_events (production_id TEXT NOT NULL, event_id TEXT NOT NULL, credential_id TEXT NOT NULL, payload_json BLOB NOT NULL, PRIMARY KEY(production_id,event_id), FOREIGN KEY(production_id) REFERENCES productions(id) ON DELETE CASCADE)`,
		`CREATE TABLE IF NOT EXISTS idempotency_results (production_id TEXT NOT NULL, key TEXT NOT NULL, action TEXT NOT NULL, revision INTEGER NOT NULL, snapshot_json BLOB NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(production_id,key))`,
		`CREATE TABLE IF NOT EXISTS idempotency_failures (production_id TEXT NOT NULL, key TEXT NOT NULL, action TEXT NOT NULL, error_json BLOB NOT NULL, created_at TEXT NOT NULL, PRIMARY KEY(production_id,key))`,
		`CREATE TABLE IF NOT EXISTS audit_events (id INTEGER PRIMARY KEY AUTOINCREMENT, production_id TEXT NOT NULL, revision INTEGER NOT NULL, actor_id TEXT NOT NULL, action TEXT NOT NULL, detail TEXT NOT NULL, created_at TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_production_revision ON audit_events(production_id,revision)`,
		`CREATE INDEX IF NOT EXISTS idx_evidence_cue ON rehearsal_evidence(production_id,cue_id)`,
		`CREATE TRIGGER IF NOT EXISTS audit_events_no_update BEFORE UPDATE ON audit_events BEGIN SELECT RAISE(ABORT,'audit events are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS audit_events_no_delete BEFORE DELETE ON audit_events BEGIN SELECT RAISE(ABORT,'audit events are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS idempotency_no_update BEFORE UPDATE ON idempotency_results BEGIN SELECT RAISE(ABORT,'idempotency results are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS idempotency_no_delete BEFORE DELETE ON idempotency_results BEGIN SELECT RAISE(ABORT,'idempotency results are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS idempotency_failures_no_update BEFORE UPDATE ON idempotency_failures BEGIN SELECT RAISE(ABORT,'idempotency failures are immutable'); END`,
		`CREATE TRIGGER IF NOT EXISTS idempotency_failures_no_delete BEFORE DELETE ON idempotency_failures BEGIN SELECT RAISE(ABORT,'idempotency failures are immutable'); END`,
	}
	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("schema migration: %w", err)
		}
	}
	var version int
	if err := s.db.QueryRowContext(ctx, `SELECT version FROM schema_version`).Scan(&version); err != nil {
		return err
	}
	if version != schemaVersion {
		return fmt.Errorf("unsupported database schema version %d", version)
	}
	return nil
}
