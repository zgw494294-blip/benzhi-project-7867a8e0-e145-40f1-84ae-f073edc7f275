package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

type manifestMaterial struct {
	Production ProductionFile      `json:"production"`
	Elements   []RiggingElement    `json:"elements"`
	Cues       []MotionCue         `json:"cues"`
	Findings   []SafetyFinding     `json:"findings"`
	Evidence   []RehearsalEvidence `json:"evidence"`
}

func BuildManifest(snapshot Snapshot, frozenAt time.Time) (FrozenManifest, error) {
	sort.Slice(snapshot.Elements, func(i, j int) bool { return snapshot.Elements[i].ID < snapshot.Elements[j].ID })
	sort.Slice(snapshot.Cues, func(i, j int) bool {
		if snapshot.Cues[i].SequenceNo == snapshot.Cues[j].SequenceNo {
			return snapshot.Cues[i].ID < snapshot.Cues[j].ID
		}
		return snapshot.Cues[i].SequenceNo < snapshot.Cues[j].SequenceNo
	})
	sort.Slice(snapshot.Findings, func(i, j int) bool { return snapshot.Findings[i].ID < snapshot.Findings[j].ID })
	for i := range snapshot.Findings {
		sort.Strings(snapshot.Findings[i].ElementIDs)
		sort.Strings(snapshot.Findings[i].CueIDs)
	}
	sort.Slice(snapshot.Evidence, func(i, j int) bool { return snapshot.Evidence[i].ID < snapshot.Evidence[j].ID })
	snapshot.Production.UpdatedAt = time.Time{}
	raw, err := json.Marshal(manifestMaterial{snapshot.Production, snapshot.Elements, snapshot.Cues, snapshot.Findings, snapshot.Evidence})
	if err != nil {
		return FrozenManifest{}, err
	}
	sum := sha256.Sum256(raw)
	return FrozenManifest{ProductionID: snapshot.Production.ID, Revision: snapshot.Production.Revision, SHA256: hex.EncodeToString(sum[:]), Canonical: string(raw), FrozenAt: frozenAt.UTC()}, nil
}

func CredentialCode(manifestSHA, productionID string, revision int64, validAt time.Time) string {
	payload, _ := json.Marshal([]any{manifestSHA, productionID, revision, validAt.UTC().Format(time.RFC3339Nano)})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}

func CredentialCodeForIssue(manifestSHA, productionID string, revision int64, validAt time.Time, credentialID string) string {
	payload, _ := json.Marshal([]any{manifestSHA, productionID, revision, validAt.UTC().Format(time.RFC3339Nano), credentialID})
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])[:16]
}
