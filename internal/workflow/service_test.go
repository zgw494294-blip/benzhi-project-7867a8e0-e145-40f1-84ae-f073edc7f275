package workflow

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"stageclearance/internal/analyzer"
	"stageclearance/internal/domain"
	"stageclearance/internal/store"
)

func TestCompleteReleaseAndRevisionConflict(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	service := New(db, analyzer.New())
	ctx := context.Background()
	performance := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	s, err := service.CreateProduction(ctx, CreateProductionCommand{ID: "p", Title: "测试制作", VenueName: "测试剧场", StageWidthMM: 10000, StageDepthMM: 8000, PerformanceStartsAt: performance, TechnicalDirectorID: "td", IdempotencyKey: "1"})
	if err != nil {
		t.Fatal(err)
	}
	e := domain.RiggingElement{ID: "e", Kind: "hoist", Label: "葫芦", XMM: 1000, YMM: 1000, ZMM: 5000, RatedLoadKG: 1000, AppliedLoadKG: 100, SafetyFactor: 2}
	s, err = service.AddElement(ctx, "p", AddElementCommand{CommandMeta: CommandMeta{s.Production.Revision, "2", "td"}, Element: e})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.AddElement(ctx, "p", AddElementCommand{CommandMeta: CommandMeta{1, "stale", "td"}, Element: domain.RiggingElement{ID: "x", Kind: "point", Label: "过期", XMM: 1, YMM: 1, ZMM: 1, RatedLoadKG: 1, SafetyFactor: 1}})
	if err == nil {
		t.Fatal("expected revision conflict")
	}
	c := domain.MotionCue{ID: "c", SequenceNo: 1, ElementID: "e", StartsAtMS: 0, DurationMS: 1000, TargetZMM: 2000, SpeedMMPerSecond: 5000, OperatorGroup: "A", MinimumClearanceMM: 800}
	s, err = service.AddCue(ctx, "p", AddCueCommand{CommandMeta: CommandMeta{s.Production.Revision, "3", "td"}, Cue: c})
	if err != nil {
		t.Fatal(err)
	}
	s, err = service.Analyze(ctx, "p", AnalyzeCommand{CommandMeta{s.Production.Revision, "4", "td"}})
	if err != nil {
		t.Fatal(err)
	}
	ev := domain.RehearsalEvidence{ID: "ev", CueID: "c", RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "op", Result: "passed", MeasuredDeviationMM: 2}
	s, err = service.AddEvidence(ctx, "p", AddEvidenceCommand{CommandMeta: CommandMeta{s.Production.Revision, "5", "op"}, Evidence: ev})
	if err != nil {
		t.Fatal(err)
	}
	s, err = service.SubmitRehearsal(ctx, "p", CommandMeta{s.Production.Revision, "6", "op"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = service.Review(ctx, "p", ReviewCommand{CommandMeta: CommandMeta{s.Production.Revision, "bad-review", "td"}, Decision: "approve", Comments: map[string]string{}}); err == nil {
		t.Fatal("latest editor must not review")
	}
	s, err = service.Review(ctx, "p", ReviewCommand{CommandMeta: CommandMeta{s.Production.Revision, "7", "reviewer"}, Decision: "approve", Comments: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	s, err = service.Release(ctx, "p", ReleaseCommand{CommandMeta: CommandMeta{s.Production.Revision, "8", "td"}, ValidPerformanceStartsAt: performance})
	if err != nil {
		t.Fatal(err)
	}
	verification, err := service.Verify(ctx, s.Credentials[0].VerificationCode)
	if err != nil || !verification.Valid || !verification.ContentMatches {
		t.Fatalf("verification=%#v err=%v", verification, err)
	}
	s, err = service.RevokeCredential(ctx, "p", s.Credentials[0].ID, RevokeCredentialCommand{CommandMeta: CommandMeta{s.Production.Revision, "9", "td"}, Reason: "校验码泄露"})
	if err != nil {
		t.Fatal(err)
	}
	verification, err = service.Verify(ctx, s.Credentials[0].VerificationCode)
	if err != nil || verification.Valid || verification.Status != "revoked" {
		t.Fatalf("revoked verification=%#v err=%v", verification, err)
	}
	s, err = service.ReissueCredential(ctx, "p", s.Credentials[0].ID, ReissueCredentialCommand{CommandMeta: CommandMeta{s.Production.Revision, "10", "td"}, Reason: "受控换发"})
	if err != nil || len(s.Credentials) != 2 || s.Credentials[0].VerificationCode == s.Credentials[1].VerificationCode {
		t.Fatalf("reissue=%#v err=%v", s.Credentials, err)
	}
	verification, err = service.Verify(ctx, s.Credentials[1].VerificationCode)
	if err != nil || !verification.Valid {
		t.Fatalf("replacement verification=%#v err=%v", verification, err)
	}
	audit, err := service.Audit(ctx, "p")
	if err != nil || len(audit) != 10 {
		t.Fatalf("audit count=%d err=%v", len(audit), err)
	}
}

func TestCueCycleRejected(t *testing.T) {
	p := domain.ProductionFile{ID: "p", StageWidthMM: 10, StageDepthMM: 10}
	elements := []domain.RiggingElement{{ID: "e"}}
	cues := []domain.MotionCue{{ID: "a", ElementID: "e", SequenceNo: 1, StartsAtMS: 0, DurationMS: 1, SpeedMMPerSecond: 1, OperatorGroup: "A", PreconditionCueIDs: []string{"b"}}}
	err := domain.ValidateCue(domain.MotionCue{ID: "b", ElementID: "e", SequenceNo: 2, StartsAtMS: 2, DurationMS: 1, SpeedMMPerSecond: 1, OperatorGroup: "A", PreconditionCueIDs: []string{"a"}}, elements, cues)
	if err == nil {
		t.Fatal("expected dependency cycle rejection")
	}
	_ = p
}
