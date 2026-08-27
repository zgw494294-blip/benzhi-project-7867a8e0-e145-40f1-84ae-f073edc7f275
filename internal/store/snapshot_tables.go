package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"stageclearance/internal/domain"
)

// syncTables 将聚合快照同步到可查询的规范化表，并保留分析、证据和凭据历史。
func syncTables(ctx context.Context, tx *sql.Tx, snapshot domain.Snapshot) error {
	for _, table := range []string{"rigging_elements", "motion_cues", "analysis_findings"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE production_id=?`, snapshot.Production.ID); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Elements {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT INTO rigging_elements VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, item.Revision, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Cues {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT INTO motion_cues VALUES(?,?,?,?,?)`, snapshot.Production.ID, item.ID, item.SequenceNo, item.Revision, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Findings {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT INTO analysis_findings VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, item.AnalysisRevision, raw); err != nil {
			return err
		}
	}
	if snapshot.Production.LastAnalysisRevision > 0 {
		raw, _ := json.Marshal(snapshot.Findings)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO analysis_batches VALUES(?,?,?,?)`, snapshot.Production.ID, snapshot.Production.LastAnalysisRevision, raw, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for _, batch := range snapshot.AnalysisBatches {
		raw, _ := json.Marshal(batch.Findings)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO analysis_batches VALUES(?,?,?,?)`, snapshot.Production.ID, batch.AnalysisRevision, raw, batch.ExecutedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Evidence {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO rehearsal_evidence VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, item.CueID, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Reviews {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO review_decisions VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, raw, item.DecidedAt.Format(time.RFC3339Nano)); err != nil {
			return err
		}
	}
	if snapshot.Manifest != nil {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO frozen_manifests VALUES(?,?,?,?)`, snapshot.Production.ID, snapshot.Manifest.Revision, snapshot.Manifest.SHA256, []byte(snapshot.Manifest.Canonical)); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Credentials {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO release_credentials VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, item.VerificationCode, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.Remediations {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO finding_remediations VALUES(?,?,?)`, snapshot.Production.ID, item.ID, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.ReviewItems {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO review_response_items VALUES(?,?,?)`, snapshot.Production.ID, item.ID, raw); err != nil {
			return err
		}
	}
	for _, item := range snapshot.CredentialEvents {
		raw, _ := json.Marshal(item)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credential_status_events VALUES(?,?,?,?)`, snapshot.Production.ID, item.ID, item.CredentialID, raw); err != nil {
			return err
		}
	}
	return nil
}
