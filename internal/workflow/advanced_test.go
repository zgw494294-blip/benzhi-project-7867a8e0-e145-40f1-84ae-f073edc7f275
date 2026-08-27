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

func TestCloneRemapsRelationsAndStartsClean(t *testing.T) {
	service, closeStore := testService(t)
	defer closeStore()
	ctx := context.Background()
	snapshot := createTestProduction(t, service, "source")
	parent := domain.RiggingElement{ID: "parent", Kind: "hoist", Label: "上级葫芦", XMM: 100, YMM: 100, ZMM: 5000, RatedLoadKG: 1000, AppliedLoadKG: 100, SafetyFactor: 2}
	snapshot = addTestElement(t, service, snapshot, parent, "element-parent")
	child := domain.RiggingElement{ID: "child", Kind: "load", Label: "载荷", XMM: 100, YMM: 100, ZMM: 4000, RatedLoadKG: 500, AppliedLoadKG: 100, SafetyFactor: 2, ParentElementID: "parent"}
	snapshot = addTestElement(t, service, snapshot, child, "element-child")
	first := domain.MotionCue{ID: "cue-a", SequenceNo: 1, ElementID: "child", StartsAtMS: 0, DurationMS: 1000, TargetZMM: 3000, SpeedMMPerSecond: 2000, OperatorGroup: "A", MinimumClearanceMM: 800}
	snapshot = addTestCue(t, service, snapshot, first, "cue-a-key")
	second := domain.MotionCue{ID: "cue-b", SequenceNo: 2, ElementID: "child", StartsAtMS: 2000, DurationMS: 1000, TargetZMM: 3500, SpeedMMPerSecond: 2000, OperatorGroup: "A", MinimumClearanceMM: 800, PreconditionCueIDs: []string{"cue-a"}}
	snapshot = addTestCue(t, service, snapshot, second, "cue-b-key")
	cloned, err := service.CloneProduction(ctx, "source", CloneProductionCommand{Title: "复制场次", PerformanceStartsAt: time.Now().UTC().Add(48 * time.Hour), ActorID: "td", IdempotencyKey: "clone"})
	if err != nil {
		t.Fatal(err)
	}
	if cloned.Production.State != domain.StateDraft || cloned.Production.Revision != 1 || cloned.Production.ID == snapshot.Production.ID {
		t.Fatalf("unexpected cloned production: %#v", cloned.Production)
	}
	if len(cloned.Findings) != 0 || len(cloned.Evidence) != 0 || len(cloned.Reviews) != 0 || cloned.Manifest != nil || len(cloned.Credentials) != 0 {
		t.Fatal("clone carried release workflow data")
	}
	if cloned.Elements[0].ID == "parent" || cloned.Elements[1].ID == "child" {
		t.Fatal("element IDs were not regenerated")
	}
	byLabel := map[string]domain.RiggingElement{}
	for _, element := range cloned.Elements {
		byLabel[element.Label] = element
	}
	if byLabel["载荷"].ParentElementID != byLabel["上级葫芦"].ID {
		t.Fatal("parent relation was not remapped")
	}
	bySequence := map[int]domain.MotionCue{}
	for _, cue := range cloned.Cues {
		bySequence[cue.SequenceNo] = cue
	}
	if bySequence[2].PreconditionCueIDs[0] != bySequence[1].ID || bySequence[1].ID == "cue-a" {
		t.Fatal("cue relation was not remapped")
	}
	_, firstFailure := service.CloneProduction(ctx, "source", CloneProductionCommand{Title: "无效场次", ActorID: "td", IdempotencyKey: "clone-failure"})
	_, repeatedFailure := service.CloneProduction(ctx, "source", CloneProductionCommand{Title: "本次输入虽有效仍应返回首次失败", PerformanceStartsAt: time.Now().UTC().Add(72 * time.Hour), ActorID: "td", IdempotencyKey: "clone-failure"})
	if firstFailure == nil || repeatedFailure == nil || firstFailure.Error() != repeatedFailure.Error() {
		t.Fatalf("failed clone result was not stable: %v / %v", firstFailure, repeatedFailure)
	}
	again, err := service.CloneProduction(ctx, "source", CloneProductionCommand{Title: "ignored", ActorID: "td", IdempotencyKey: "clone"})
	if err != nil || again.Production.ID != cloned.Production.ID {
		t.Fatalf("clone idempotency failed: %v", err)
	}
}

func TestBatchAndScheduleAreAtomic(t *testing.T) {
	service, closeStore := testService(t)
	defer closeStore()
	ctx := context.Background()
	snapshot := createTestProduction(t, service, "batch")
	element := domain.RiggingElement{ID: "e", Kind: "hoist", Label: "葫芦", XMM: 100, YMM: 100, ZMM: 5000, RatedLoadKG: 1000, AppliedLoadKG: 100, SafetyFactor: 2}
	snapshot = addTestElement(t, service, snapshot, element, "e")
	before := snapshot.Production.Revision
	_, err := service.BatchElements(ctx, "batch", BatchElementsCommand{CommandMeta: CommandMeta{ExpectedRevision: before, IdempotencyKey: "bad-batch", ActorID: "td"}, Changes: []ElementChange{{Operation: "add", Element: &domain.RiggingElement{ID: "outside", Kind: "load", Label: "越界", XMM: 20000, YMM: 1, ZMM: 1, RatedLoadKG: 1, SafetyFactor: 1}}, {Operation: "update", ElementID: "e", Element: &domain.RiggingElement{Kind: "hoist", Label: "不应保存", XMM: 1, YMM: 1, ZMM: 1, RatedLoadKG: 1, SafetyFactor: 1}}}})
	if err == nil {
		t.Fatal("expected atomic batch rejection")
	}
	after, _ := service.Get(ctx, "batch")
	if after.Production.Revision != before || after.Elements[0].Label != "葫芦" || len(after.Elements) != 1 {
		t.Fatal("failed batch was partially saved")
	}
	first := domain.MotionCue{ID: "a", SequenceNo: 1, ElementID: "e", StartsAtMS: 1000, DurationMS: 500, TargetZMM: 4500, SpeedMMPerSecond: 1000, OperatorGroup: "A"}
	after = addTestCue(t, service, after, first, "a")
	second := domain.MotionCue{ID: "b", SequenceNo: 2, ElementID: "e", StartsAtMS: 2000, DurationMS: 500, TargetZMM: 4500, SpeedMMPerSecond: 1000, OperatorGroup: "A", PreconditionCueIDs: []string{"a"}}
	after = addTestCue(t, service, after, second, "b")
	before = after.Production.Revision
	_, err = service.ScheduleCues(ctx, "batch", ScheduleCuesCommand{CommandMeta: CommandMeta{ExpectedRevision: before, IdempotencyKey: "bad-schedule", ActorID: "td"}, OffsetMS: -3000, Changes: []CueScheduleChange{{CueID: "a"}, {CueID: "b"}}})
	if err == nil {
		t.Fatal("expected negative schedule rejection")
	}
	unchanged, _ := service.Get(ctx, "batch")
	if unchanged.Production.Revision != before || unchanged.Cues[0].StartsAtMS != 1000 {
		t.Fatal("failed schedule was partially saved")
	}
}

func TestAnalysisHistoryRemediationAndEvidenceRules(t *testing.T) {
	service, closeStore := testService(t)
	defer closeStore()
	ctx := context.Background()
	snapshot := createTestProduction(t, service, "analysis")
	element := domain.RiggingElement{ID: "e", Kind: "hoist", Label: "超载葫芦", XMM: 100, YMM: 100, ZMM: 5000, RatedLoadKG: 1000, AppliedLoadKG: 600, SafetyFactor: 2}
	snapshot = addTestElement(t, service, snapshot, element, "e")
	cue := domain.MotionCue{ID: "cue", SequenceNo: 1, ElementID: "e", StartsAtMS: 0, DurationMS: 1000, TargetZMM: 4500, SpeedMMPerSecond: 1000, OperatorGroup: "A", MinimumClearanceMM: 800}
	snapshot = addTestCue(t, service, snapshot, cue, "cue")
	var err error
	snapshot, err = service.Analyze(ctx, "analysis", AnalyzeCommand{CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: "analysis-1", ActorID: "td"}})
	if err != nil || snapshot.Production.State != domain.StateRemediation {
		t.Fatalf("first analysis: %v", err)
	}
	findingID := snapshot.Findings[0].ID
	snapshot, err = service.Remediate(ctx, "analysis", RemediateCommand{CommandMeta: CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: "remediate", ActorID: "td"}, FindingID: findingID, OwnerID: "td", Measure: "降低载荷", TargetCueID: "cue"})
	if err != nil {
		t.Fatal(err)
	}
	element.AppliedLoadKG = 100
	snapshot, err = service.UpdateElement(ctx, "analysis", "e", AddElementCommand{CommandMeta: CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: "fix", ActorID: "td"}, Element: element})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.Analyze(ctx, "analysis", AnalyzeCommand{CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: "analysis-2", ActorID: "td"}})
	if err != nil || snapshot.Production.State != domain.StatePendingRehearsal || snapshot.Remediations[0].Status != "awaiting_rehearsal" {
		t.Fatalf("second analysis: %#v %v", snapshot.Remediations, err)
	}
	comparison, err := service.CompareAnalysis(ctx, "analysis", snapshot.AnalysisBatches[0].AnalysisRevision, snapshot.AnalysisBatches[1].AnalysisRevision)
	if err != nil || len(comparison.Changes) == 0 || comparison.Changes[0].Status != "resolved" {
		t.Fatalf("comparison=%#v err=%v", comparison, err)
	}
	evidence := domain.RehearsalEvidence{ID: "ev", CueID: "cue", RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "op", Result: "passed"}
	snapshot, err = service.AddEvidence(ctx, "analysis", AddEvidenceCommand{CommandMeta: CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: "ev", ActorID: "op"}, Evidence: evidence})
	if err != nil || snapshot.Remediations[0].Status != "closed" {
		t.Fatalf("evidence close: %v", err)
	}
	before := snapshot.Production.Revision
	_, err = service.AddEvidence(ctx, "analysis", AddEvidenceCommand{CommandMeta: CommandMeta{ExpectedRevision: before, IdempotencyKey: "duplicate-run", ActorID: "op"}, Evidence: domain.RehearsalEvidence{ID: "ev2", CueID: "cue", RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "op", Result: "passed"}})
	if err == nil {
		t.Fatal("expected duplicate run rejection")
	}
	after, _ := service.Get(ctx, "analysis")
	if after.Production.Revision != before || len(after.Evidence) != 1 {
		t.Fatal("failed evidence changed snapshot")
	}
}

func TestReturnedReviewRequiresCurrentResponses(t *testing.T) {
	service, closeStore := testService(t)
	defer closeStore()
	ctx := context.Background()
	snapshot := createTestProduction(t, service, "review-return")
	element := domain.RiggingElement{ID: "e", Kind: "hoist", Label: "葫芦", XMM: 100, YMM: 100, ZMM: 5000, RatedLoadKG: 1000, AppliedLoadKG: 100, SafetyFactor: 1.2}
	snapshot = addTestElement(t, service, snapshot, element, "e")
	cue := domain.MotionCue{ID: "cue", SequenceNo: 1, ElementID: "e", StartsAtMS: 0, DurationMS: 1000, TargetZMM: 4500, SpeedMMPerSecond: 1000, OperatorGroup: "A", MinimumClearanceMM: 800}
	snapshot = addTestCue(t, service, snapshot, cue, "cue")
	var err error
	snapshot, err = service.Analyze(ctx, "review-return", AnalyzeCommand{CommandMeta{snapshot.Production.Revision, "analysis-a", "td"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.AddEvidence(ctx, "review-return", AddEvidenceCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "evidence-a", "op"}, Evidence: domain.RehearsalEvidence{ID: "evidence-a", CueID: "cue", RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "op", Result: "passed"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.SubmitRehearsal(ctx, "review-return", CommandMeta{snapshot.Production.Revision, "submit-a", "op"})
	if err != nil {
		t.Fatal(err)
	}
	findingID := snapshot.Findings[0].ID
	snapshot, err = service.Review(ctx, "review-return", ReviewCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "return", "reviewer"}, Decision: "return", Comments: map[string]string{findingID: "请提高安全系数"}, Assignees: map[string]string{findingID: "td"}})
	if err != nil || len(snapshot.ReviewItems) != 1 {
		t.Fatalf("return review: %v", err)
	}
	element.SafetyFactor = 2
	snapshot, err = service.UpdateElement(ctx, "review-return", "e", AddElementCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "fix-review", "td"}, Element: element})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.Analyze(ctx, "review-return", AnalyzeCommand{CommandMeta{snapshot.Production.Revision, "analysis-b", "td"}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.AddEvidence(ctx, "review-return", AddEvidenceCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "evidence-b", "op"}, Evidence: domain.RehearsalEvidence{ID: "evidence-b", CueID: "cue", RunNumber: 1, PerformedAt: time.Now().UTC(), OperatorID: "op", Result: "passed"}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.SubmitRehearsal(ctx, "review-return", CommandMeta{snapshot.Production.Revision, "submit-incomplete", "op"})
	if err == nil {
		t.Fatal("expected unanswered review item rejection")
	}
	snapshot, err = service.RespondReviewItem(ctx, "review-return", snapshot.ReviewItems[0].ID, ReviewResponseCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "response", "td"}, Response: "已提高安全系数并复演", RelatedAnalysisRevision: snapshot.Production.LastAnalysisRevision, RelatedEvidenceID: "evidence-b"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.SubmitRehearsal(ctx, "review-return", CommandMeta{snapshot.Production.Revision, "submit-b", "op"})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err = service.Review(ctx, "review-return", ReviewCommand{CommandMeta: CommandMeta{snapshot.Production.Revision, "approve-b", "reviewer-2"}, Decision: "approve", Comments: map[string]string{}})
	if err != nil || snapshot.ReviewItems[0].Status != "closed" || len(snapshot.Reviews) != 2 {
		t.Fatalf("approve after responses: %#v %v", snapshot.ReviewItems, err)
	}
}

func testService(t *testing.T) (*Service, func()) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "advanced.db"))
	if err != nil {
		t.Fatal(err)
	}
	return New(db, analyzer.New()), func() { _ = db.Close() }
}

func createTestProduction(t *testing.T, service *Service, id string) domain.Snapshot {
	t.Helper()
	snapshot, err := service.CreateProduction(context.Background(), CreateProductionCommand{ID: id, Title: "测试", VenueName: "剧场", StageWidthMM: 10000, StageDepthMM: 8000, PerformanceStartsAt: time.Now().UTC().Add(time.Hour), TechnicalDirectorID: "td", IdempotencyKey: "create-" + id})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func addTestElement(t *testing.T, service *Service, snapshot domain.Snapshot, element domain.RiggingElement, key string) domain.Snapshot {
	t.Helper()
	result, err := service.AddElement(context.Background(), snapshot.Production.ID, AddElementCommand{CommandMeta: CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: key, ActorID: "td"}, Element: element})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func addTestCue(t *testing.T, service *Service, snapshot domain.Snapshot, cue domain.MotionCue, key string) domain.Snapshot {
	t.Helper()
	result, err := service.AddCue(context.Background(), snapshot.Production.ID, AddCueCommand{CommandMeta: CommandMeta{ExpectedRevision: snapshot.Production.Revision, IdempotencyKey: key, ActorID: "td"}, Cue: cue})
	if err != nil {
		t.Fatal(err)
	}
	return result
}
