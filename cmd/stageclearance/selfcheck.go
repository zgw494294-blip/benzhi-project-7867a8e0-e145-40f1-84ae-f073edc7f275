package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"stageclearance/internal/domain"
)

func executeSelfcheck(ctx context.Context, baseURL string) error {
	client := &http.Client{Timeout: 4 * time.Second}
	performance := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	var snapshot domain.Snapshot
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions", map[string]any{"id": "selfcheck-production", "title": "自检演出", "venueName": "回环测试剧场", "stageWidthMM": 12000, "stageDepthMM": 9000, "performanceStartsAt": performance, "technicalDirectorID": "td-selfcheck", "idempotencyKey": "selfcheck-create"}, &snapshot); err != nil {
		return err
	}
	element := domain.RiggingElement{ID: "selfcheck-hoist", Kind: "hoist", Label: "自检葫芦", XMM: 2000, YMM: 2000, ZMM: 6000, RatedLoadKG: 1000, AppliedLoadKG: 200, SafetyFactor: 2}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/elements", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-element", "actorID": "td-selfcheck", "element": element}, &snapshot); err != nil {
		return err
	}
	cue := domain.MotionCue{ID: "selfcheck-cue", SequenceNo: 1, ElementID: element.ID, StartsAtMS: 1000, DurationMS: 2000, TargetZMM: 3000, SpeedMMPerSecond: 2000, OperatorGroup: "自检操作组", PreconditionCueIDs: []string{}, MinimumClearanceMM: 1000}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/cues", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-cue", "actorID": "td-selfcheck", "cue": cue}, &snapshot); err != nil {
		return err
	}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/analysis", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-analysis", "actorID": "td-selfcheck"}, &snapshot); err != nil {
		return err
	}
	if snapshot.Production.State != domain.StatePendingRehearsal {
		return fmt.Errorf("分析后状态为 %s", snapshot.Production.State)
	}
	evidence := domain.RehearsalEvidence{ID: "selfcheck-evidence", CueID: cue.ID, RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "operator-selfcheck", Result: "passed", MeasuredDeviationMM: 4}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/evidence", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-evidence", "actorID": "operator-selfcheck", "evidence": evidence}, &snapshot); err != nil {
		return err
	}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/rehearsal/submit", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-submit", "actorID": "operator-selfcheck"}, &snapshot); err != nil {
		return err
	}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/reviews", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-review", "actorID": "reviewer-selfcheck", "decision": "approve", "comments": map[string]string{}}, &snapshot); err != nil {
		return err
	}
	if snapshot.Manifest == nil {
		return fmt.Errorf("复核后未生成冻结清单")
	}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/productions/selfcheck-production/release", map[string]any{"expectedRevision": snapshot.Production.Revision, "idempotencyKey": "selfcheck-release", "actorID": "td-selfcheck", "validPerformanceStartsAt": performance}, &snapshot); err != nil {
		return err
	}
	if len(snapshot.Credentials) != 1 {
		return fmt.Errorf("未签发唯一凭据")
	}
	var verification struct {
		Valid          bool `json:"valid"`
		ContentMatches bool `json:"contentMatches"`
	}
	if err := selfcheckRequest(ctx, client, "POST", baseURL+"/api/credentials/verify", map[string]any{"code": snapshot.Credentials[0].VerificationCode}, &verification); err != nil {
		return err
	}
	if !verification.Valid || !verification.ContentMatches {
		return fmt.Errorf("凭据校验未通过")
	}
	return nil
}

func selfcheckRequest(ctx context.Context, client *http.Client, method, url string, input, output any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2<<20))
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s %s 返回 %d: %s", method, url, response.StatusCode, string(body))
	}
	if err = json.Unmarshal(body, output); err != nil {
		return fmt.Errorf("解析 %s: %w", url, err)
	}
	return nil
}
