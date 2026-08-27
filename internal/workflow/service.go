package workflow

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"stageclearance/internal/analyzer"
	"stageclearance/internal/domain"
	"stageclearance/internal/store"
)

type Service struct {
	store        *store.Store
	analyzer     *analyzer.Analyzer
	now          func() time.Time
	historyMu    sync.RWMutex
	historyCache map[string][]byte
}

func New(s *store.Store, a *analyzer.Analyzer) *Service {
	return &Service{store: s, analyzer: a, now: func() time.Time { return time.Now().UTC() }, historyCache: make(map[string][]byte)}
}

type CreateProductionCommand struct {
	ID                  string    `json:"id"`
	Title               string    `json:"title"`
	VenueName           string    `json:"venueName"`
	StageWidthMM        float64   `json:"stageWidthMM"`
	StageDepthMM        float64   `json:"stageDepthMM"`
	PerformanceStartsAt time.Time `json:"performanceStartsAt"`
	TechnicalDirectorID string    `json:"technicalDirectorID"`
	IdempotencyKey      string    `json:"idempotencyKey"`
}

type CommandMeta struct {
	ExpectedRevision int64  `json:"expectedRevision"`
	IdempotencyKey   string `json:"idempotencyKey"`
	ActorID          string `json:"actorID"`
}

type AddElementCommand struct {
	CommandMeta
	Element domain.RiggingElement `json:"element"`
}
type AddCueCommand struct {
	CommandMeta
	Cue domain.MotionCue `json:"cue"`
}
type AnalyzeCommand struct{ CommandMeta }
type RemediateCommand struct {
	CommandMeta
	FindingID   string `json:"findingID"`
	Note        string `json:"note,omitempty"`
	OwnerID     string `json:"ownerID"`
	Measure     string `json:"measure"`
	TargetCueID string `json:"targetCueID"`
}
type AddEvidenceCommand struct {
	CommandMeta
	Evidence domain.RehearsalEvidence `json:"evidence"`
}
type ReviewCommand struct {
	CommandMeta
	Decision  string            `json:"decision"`
	Comments  map[string]string `json:"comments"`
	Assignees map[string]string `json:"assignees,omitempty"`
}
type ReleaseCommand struct {
	CommandMeta
	ValidPerformanceStartsAt time.Time `json:"validPerformanceStartsAt"`
}

type Verification struct {
	Valid               bool      `json:"valid"`
	ContentMatches      bool      `json:"contentMatches"`
	Status              string    `json:"status"`
	ProductionID        string    `json:"productionID,omitempty"`
	PerformanceStartsAt time.Time `json:"performanceStartsAt,omitempty"`
	Message             string    `json:"message"`
}

func (s *Service) CreateProduction(ctx context.Context, c CreateProductionCommand) (domain.Snapshot, error) {
	if c.ID == "" {
		c.ID = newID("prod")
	}
	now := s.now()
	p := domain.ProductionFile{ID: c.ID, Title: strings.TrimSpace(c.Title), VenueName: strings.TrimSpace(c.VenueName), StageWidthMM: c.StageWidthMM, StageDepthMM: c.StageDepthMM, PerformanceStartsAt: c.PerformanceStartsAt.UTC(), TechnicalDirectorID: strings.TrimSpace(c.TechnicalDirectorID), State: domain.StateDraft, Revision: 1, LastEditorID: strings.TrimSpace(c.TechnicalDirectorID), CreatedAt: now, UpdatedAt: now}
	if err := domain.ValidateProduction(p); err != nil {
		return domain.Snapshot{}, err
	}
	snapshot := domain.Snapshot{Production: p, Elements: []domain.RiggingElement{}, Cues: []domain.MotionCue{}, Findings: []domain.SafetyFinding{}, Evidence: []domain.RehearsalEvidence{}, Reviews: []domain.ReviewDecision{}, Credentials: []domain.ReleaseCredential{}, AnalysisBatches: []domain.AnalysisBatch{}, Remediations: []domain.FindingRemediation{}, ReviewItems: []domain.ReviewResponseItem{}, CredentialEvents: []domain.CredentialStatusEvent{}}
	return s.store.Create(ctx, snapshot, c.IdempotencyKey, p.TechnicalDirectorID, "production.create")
}

func (s *Service) Get(ctx context.Context, id string) (domain.Snapshot, error) {
	return s.store.Get(ctx, id)
}
func (s *Service) List(ctx context.Context) ([]domain.Snapshot, error) { return s.store.List(ctx) }
func (s *Service) Audit(ctx context.Context, id string) ([]store.AuditEvent, error) {
	return s.store.Audit(ctx, id)
}

func (s *Service) AddElement(ctx context.Context, id string, c AddElementCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "element.add", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "编辑吊挂方案")
		}
		if c.ActorID == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		e := c.Element
		if e.ID == "" {
			e.ID = newID("element")
		}
		e.ProductionID = id
		e.Revision = c.ExpectedRevision + 1
		for _, existing := range snapshot.Elements {
			if existing.ID == e.ID {
				return domain.Invalid("element.id", "吊挂元素标识已存在")
			}
		}
		if err := domain.ValidateElement(e, snapshot.Production, snapshot.Elements); err != nil {
			return err
		}
		snapshot.Elements = append(snapshot.Elements, e)
		markPlanEdited(snapshot, c.ActorID)
		return nil
	})
}

func (s *Service) UpdateElement(ctx context.Context, id, elementID string, c AddElementCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "element.update", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "修改吊挂方案")
		}
		if c.ActorID == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		index := -1
		for i, item := range snapshot.Elements {
			if item.ID == elementID {
				index = i
				break
			}
		}
		if index < 0 {
			return domain.NotFound("element", elementID)
		}
		e := c.Element
		e.ID = elementID
		e.ProductionID = id
		e.Revision = c.ExpectedRevision + 1
		others := append([]domain.RiggingElement(nil), snapshot.Elements[:index]...)
		others = append(others, snapshot.Elements[index+1:]...)
		if err := domain.ValidateElement(e, snapshot.Production, others); err != nil {
			return err
		}
		snapshot.Elements[index] = e
		markPlanEdited(snapshot, c.ActorID)
		return nil
	})
}

func (s *Service) AddCue(ctx context.Context, id string, c AddCueCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "cue.add", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "编排动作提示")
		}
		if c.ActorID == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		cue := c.Cue
		if cue.ID == "" {
			cue.ID = newID("cue")
		}
		cue.ProductionID = id
		cue.Revision = c.ExpectedRevision + 1
		for _, existing := range snapshot.Cues {
			if existing.ID == cue.ID {
				return domain.Invalid("cue.id", "提示标识已存在")
			}
			if existing.SequenceNo == cue.SequenceNo {
				return domain.Invalid("sequenceNo", "提示序号不能重复")
			}
		}
		if err := domain.ValidateCue(cue, snapshot.Elements, snapshot.Cues); err != nil {
			return err
		}
		snapshot.Cues = append(snapshot.Cues, cue)
		markPlanEdited(snapshot, c.ActorID)
		return nil
	})
}

func (s *Service) UpdateCue(ctx context.Context, id, cueID string, c AddCueCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "cue.update", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "修改动作提示")
		}
		if c.ActorID == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		index := -1
		for i, item := range snapshot.Cues {
			if item.ID == cueID {
				index = i
				break
			}
		}
		if index < 0 {
			return domain.NotFound("cue", cueID)
		}
		cue := c.Cue
		cue.ID = cueID
		cue.ProductionID = id
		cue.Revision = c.ExpectedRevision + 1
		others := append([]domain.MotionCue(nil), snapshot.Cues[:index]...)
		others = append(others, snapshot.Cues[index+1:]...)
		for _, item := range others {
			if item.SequenceNo == cue.SequenceNo {
				return domain.Invalid("sequenceNo", "提示序号不能重复")
			}
		}
		if err := domain.ValidateCue(cue, snapshot.Elements, others); err != nil {
			return err
		}
		snapshot.Cues[index] = cue
		markPlanEdited(snapshot, c.ActorID)
		return nil
	})
}

func (s *Service) Analyze(ctx context.Context, id string, c AnalyzeCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "analysis.run", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "提交安全分析")
		}
		if len(snapshot.Elements) == 0 {
			return domain.Invalid("elements", "至少登记一个吊挂元素")
		}
		if len(snapshot.Cues) == 0 {
			return domain.Invalid("cues", "至少编排一个动作提示")
		}
		if err := domain.Transition(&snapshot.Production, domain.StatePendingAnalysis, "进入待分析"); err != nil {
			return err
		}
		analysisCopy := *snapshot
		analysisCopy.Production.Revision = c.ExpectedRevision + 1
		findings := s.analyzer.Analyze(analysisCopy)
		analysisRevision := c.ExpectedRevision + 1
		snapshot.Findings = findings
		snapshot.Production.LastAnalysisRevision = analysisRevision
		snapshot.AnalysisBatches = append(snapshot.AnalysisBatches, domain.AnalysisBatch{ID: newID("analysis"), ProductionID: id, AnalysisRevision: analysisRevision, RuleVersion: domain.AnalysisRuleVersion, ExecutedAt: s.now(), Findings: append([]domain.SafetyFinding(nil), findings...)})
		currentByKey := map[string]domain.SafetyFinding{}
		for _, finding := range findings {
			currentByKey[domain.FindingIdentity(finding)] = finding
		}
		for i := range snapshot.Remediations {
			remediation := &snapshot.Remediations[i]
			if remediation.Status == "closed" {
				continue
			}
			if current, exists := currentByKey[remediation.FindingKey]; exists {
				remediation.Status, remediation.LatestFindingID, remediation.ResolvedAnalysisRevision = "in_progress", current.ID, 0
				for findingIndex := range snapshot.Findings {
					if snapshot.Findings[findingIndex].ID == current.ID {
						snapshot.Findings[findingIndex].ResolutionState, snapshot.Findings[findingIndex].RemediationNote = "remediating", remediation.Measure
					}
				}
			} else if analysisRevision > remediation.BaselineAnalysisRevision {
				remediation.Status, remediation.LatestFindingID, remediation.ResolvedAnalysisRevision = "awaiting_rehearsal", "", analysisRevision
			}
		}
		blocking := false
		for _, f := range findings {
			if f.Severity == "blocking" {
				blocking = true
				break
			}
		}
		if blocking {
			snapshot.Production.State = domain.StateRemediation
		} else {
			snapshot.Production.State = domain.StatePendingRehearsal
		}
		return nil
	})
}

func (s *Service) Remediate(ctx context.Context, id string, c RemediateCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "finding.remediate", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StateRemediation {
			return domain.StateConflict(snapshot.Production.State, "记录整改")
		}
		measure := strings.TrimSpace(c.Measure)
		if measure == "" {
			measure = strings.TrimSpace(c.Note)
		}
		if strings.TrimSpace(c.OwnerID) == "" {
			return domain.Invalid("ownerID", "整改负责人不能为空")
		}
		if measure == "" {
			return domain.Invalid("measure", "整改措施不能为空")
		}
		if c.TargetCueID == "" {
			return domain.Invalid("targetCueID", "目标复验提示不能为空")
		}
		if _, exists := snapshot.CueByID(c.TargetCueID); !exists {
			return domain.Invalid("targetCueID", "目标复验提示不存在")
		}
		found := false
		for i := range snapshot.Findings {
			if snapshot.Findings[i].ID == c.FindingID {
				if snapshot.Findings[i].Severity != "blocking" || snapshot.Findings[i].ResolutionState != "open" {
					return domain.Invalid("findingID", "发现并非当前开放阻断项")
				}
				for _, existing := range snapshot.Remediations {
					if existing.FindingID == c.FindingID && existing.Status != "closed" {
						return domain.Invalid("findingID", "该阻断发现已登记整改")
					}
				}
				snapshot.Findings[i].ResolutionState = "remediating"
				snapshot.Findings[i].RemediationNote = measure
				snapshot.Remediations = append(snapshot.Remediations, domain.FindingRemediation{ID: newID("remediation"), ProductionID: id, FindingID: c.FindingID, FindingKey: domain.FindingIdentity(snapshot.Findings[i]), OwnerID: strings.TrimSpace(c.OwnerID), Measure: measure, TargetCueID: c.TargetCueID, BaselineAnalysisRevision: snapshot.Findings[i].AnalysisRevision, Status: "in_progress", CreatedAt: s.now()})
				found = true
				break
			}
		}
		if !found {
			return domain.NotFound("finding", c.FindingID)
		}
		return nil
	})
}

func (s *Service) AddEvidence(ctx context.Context, id string, c AddEvidenceCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "evidence.add", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StatePendingRehearsal {
			return domain.StateConflict(snapshot.Production.State, "记录排练证据")
		}
		e := c.Evidence
		if e.ID == "" {
			e.ID = newID("evidence")
		}
		e.ProductionID = id
		if e.PerformedAt.IsZero() {
			e.PerformedAt = s.now()
		}
		if e.OperatorID == "" {
			e.OperatorID = c.ActorID
		}
		e.AnalysisRevision = snapshot.Production.LastAnalysisRevision
		if err := domain.ValidateEvidence(e, snapshot.Cues); err != nil {
			return err
		}
		maxRun := 0
		evidenceIDs := map[string]bool{}
		superseded := map[string]bool{}
		for _, old := range snapshot.Evidence {
			evidenceIDs[old.ID] = true
			if old.SupersedesEvidenceID != "" {
				superseded[old.SupersedesEvidenceID] = true
			}
			if old.CueID == e.CueID && old.AnalysisRevision == e.AnalysisRevision && old.RunNumber > maxRun {
				maxRun = old.RunNumber
			}
		}
		if evidenceIDs[e.ID] {
			return domain.Invalid("evidence.id", "排练证据标识已存在")
		}
		if e.RunNumber <= maxRun {
			return domain.Invalid("runNumber", fmt.Sprintf("轮次必须严格递增；当前最大轮次为 %d", maxRun))
		}
		if e.SupersedesEvidenceID != "" {
			if e.SupersedesEvidenceID == e.ID {
				return domain.Invalid("supersedesEvidenceID", "证据不能替代自身")
			}
			found := false
			for _, old := range snapshot.Evidence {
				if old.ID == e.SupersedesEvidenceID && old.CueID == e.CueID && old.AnalysisRevision == e.AnalysisRevision && !superseded[old.ID] && old.RunNumber == maxRun {
					found = true
				}
			}
			if !found {
				return domain.Invalid("supersedesEvidenceID", "只能替代同一制作、提示和分析批次中当前未被替代的末端证据")
			}
		}
		snapshot.Evidence = append(snapshot.Evidence, e)
		if e.Result == "blocked" {
			snapshot.Production.State = domain.StateRemediation
		} else {
			for i := range snapshot.Remediations {
				item := &snapshot.Remediations[i]
				if item.Status == "awaiting_rehearsal" && item.TargetCueID == e.CueID && item.ResolvedAnalysisRevision == e.AnalysisRevision {
					item.Status, item.ClosingEvidenceID, item.ClosedAt = "closed", e.ID, s.now()
				}
			}
		}
		return nil
	})
}

func (s *Service) SubmitRehearsal(ctx context.Context, id string, c CommandMeta) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "rehearsal.submit", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StatePendingRehearsal {
			return domain.StateConflict(snapshot.Production.State, "提交复核")
		}
		for _, remediation := range snapshot.Remediations {
			if remediation.Status != "closed" {
				return domain.Invalid("remediations", "仍有未完成闭环的阻断发现 "+remediation.FindingID)
			}
		}
		coverage := snapshot.EvidenceCoverage()
		if coverage.Passed != coverage.Total {
			var gaps []string
			for _, item := range coverage.Items {
				if item.Status != "passed" {
					gaps = append(gaps, item.CueID+":"+item.MissingReason)
				}
			}
			return domain.Invalid("evidence", "排练覆盖仍有缺口："+strings.Join(gaps, "；"))
		}
		if gaps := incompleteReviewItems(snapshot); len(gaps) > 0 {
			return domain.Invalid("reviewItems", "最近退回意见尚未有效回应："+strings.Join(gaps, ","))
		}
		if err := domain.Transition(&snapshot.Production, domain.StatePendingReview, "进入待复核"); err != nil {
			return err
		}
		return nil
	})
}

func (s *Service) Review(ctx context.Context, id string, c ReviewCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "review."+c.Decision, func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StatePendingReview {
			return domain.StateConflict(snapshot.Production.State, "安全复核")
		}
		if c.ActorID == "" {
			return domain.Invalid("actorID", "复核员不能为空")
		}
		if c.ActorID == snapshot.Production.LastEditorID {
			return domain.Invalid("actorID", "复核员不得参与最近一次方案编辑")
		}
		if c.Decision != "approve" && c.Decision != "return" {
			return domain.Invalid("decision", "复核决定必须为 approve 或 return")
		}
		for _, f := range snapshot.Findings {
			if _, ok := c.Comments[f.ID]; !ok {
				return domain.Invalid("comments", "必须对每项分析发现给出逐项意见")
			}
		}
		review := domain.ReviewDecision{ID: newID("review"), ReviewerID: c.ActorID, Decision: c.Decision, Comments: c.Comments, ProductionRevision: c.ExpectedRevision + 1, DecidedAt: s.now()}
		snapshot.Reviews = append(snapshot.Reviews, review)
		if c.Decision == "return" {
			if len(c.Comments) == 0 {
				return domain.Invalid("comments", "退回复核必须包含至少一项明确意见")
			}
			for findingID, comment := range c.Comments {
				if strings.TrimSpace(comment) == "" {
					return domain.Invalid("comments."+findingID, "退回意见不能为空")
				}
				if _, exists := snapshot.FindingByID(findingID); !exists {
					return domain.Invalid("comments."+findingID, "退回意见引用的分析发现不存在")
				}
				owner := strings.TrimSpace(c.Assignees[findingID])
				if owner == "" {
					return domain.Invalid("assignees."+findingID, "退回意见责任人不能为空")
				}
				snapshot.ReviewItems = append(snapshot.ReviewItems, domain.ReviewResponseItem{ID: newID("review-item"), ReviewID: review.ID, FindingID: findingID, Comment: strings.TrimSpace(comment), OwnerID: owner, Status: "open"})
			}
			snapshot.Production.State = domain.StateRemediation
			return nil
		}
		future := *snapshot
		if gaps := incompleteReviewItems(snapshot); len(gaps) > 0 {
			return domain.Invalid("reviewItems", "当前退回清单尚未完成："+strings.Join(gaps, ","))
		}
		latestReturn := ""
		for i := len(snapshot.Reviews) - 1; i >= 0; i-- {
			if snapshot.Reviews[i].Decision == "return" {
				latestReturn = snapshot.Reviews[i].ID
				break
			}
		}
		for i := range snapshot.ReviewItems {
			if snapshot.ReviewItems[i].ReviewID == latestReturn && snapshot.ReviewItems[i].Status == "responded" {
				snapshot.ReviewItems[i].Status = "closed"
			}
		}
		future.Production.Revision = c.ExpectedRevision + 1
		future.Production.State = domain.StateFrozen
		manifest, err := domain.BuildManifest(future, s.now())
		if err != nil {
			return err
		}
		snapshot.Manifest = &manifest
		snapshot.Production.State = domain.StateFrozen
		return nil
	})
}

func (s *Service) Release(ctx context.Context, id string, c ReleaseCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, c.ExpectedRevision, c.IdempotencyKey, c.ActorID, "release.issue", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StateFrozen {
			return domain.StateConflict(snapshot.Production.State, "签发放行凭据")
		}
		if c.ActorID != snapshot.Production.TechnicalDirectorID {
			return domain.Invalid("actorID", "只有技术总监可以签发凭据")
		}
		if snapshot.Manifest == nil {
			return domain.Invalid("manifest", "冻结清单不存在")
		}
		if !c.ValidPerformanceStartsAt.Equal(snapshot.Production.PerformanceStartsAt) {
			return domain.Invalid("validPerformanceStartsAt", "凭据场次必须与制作档案场次一致")
		}
		approvedBy := ""
		for i := len(snapshot.Reviews) - 1; i >= 0; i-- {
			if snapshot.Reviews[i].Decision == "approve" {
				approvedBy = snapshot.Reviews[i].ReviewerID
				break
			}
		}
		credential := domain.ReleaseCredential{ID: newID("credential"), ProductionID: id, FrozenRevision: snapshot.Manifest.Revision, ManifestSHA256: snapshot.Manifest.SHA256, ApprovedBy: approvedBy, IssuedBy: c.ActorID, ValidPerformanceStartsAt: c.ValidPerformanceStartsAt.UTC(), IssuedAt: s.now(), Status: "valid"}
		if approvedBy == "" {
			return domain.Invalid("approval", "档案没有有效批准记录")
		}
		for _, existing := range snapshot.Credentials {
			status, _, _ := snapshot.EffectiveCredentialStatus(existing.ID)
			if status == "valid" {
				return domain.Invalid("credentials", "当前已有有效放行凭据")
			}
		}
		credential.VerificationCode = domain.CredentialCode(credential.ManifestSHA256, id, credential.FrozenRevision, credential.ValidPerformanceStartsAt)
		snapshot.Credentials = append(snapshot.Credentials, credential)
		snapshot.Production.State = domain.StateReleased
		return nil
	})
}

func (s *Service) Verify(ctx context.Context, code string) (Verification, error) {
	snapshot, credential, err := s.store.FindCredential(ctx, strings.TrimSpace(code))
	if err != nil {
		return Verification{Valid: false, Status: "not_found", Message: "未找到该校验码"}, err
	}
	if snapshot.Manifest == nil {
		return Verification{Valid: false, Status: "manifest_missing", ProductionID: snapshot.Production.ID, Message: "冻结清单缺失"}, nil
	}
	matches, err := domain.VerifyManifest(snapshot, *snapshot.Manifest)
	if err != nil {
		return Verification{}, err
	}
	matches = matches && snapshot.Manifest.SHA256 == credential.ManifestSHA256
	expected := domain.CredentialCode(credential.ManifestSHA256, credential.ProductionID, credential.FrozenRevision, credential.ValidPerformanceStartsAt)
	if credential.ReplacesCredentialID != "" {
		expected = domain.CredentialCodeForIssue(credential.ManifestSHA256, credential.ProductionID, credential.FrozenRevision, credential.ValidPerformanceStartsAt, credential.ID)
	}
	status, reason, replacement := snapshot.EffectiveCredentialStatus(credential.ID)
	valid := matches && expected == credential.VerificationCode && status == "valid" && snapshot.Production.State == domain.StateReleased
	message := "凭据有效且冻结内容匹配"
	if !valid {
		switch status {
		case "revoked":
			message = "凭据已作废：" + reason
		case "replaced":
			message = "凭据已换发，新凭据 " + replacement
		default:
			message = "凭据无效或冻结内容不匹配"
		}
	}
	return Verification{Valid: valid, ContentMatches: matches, Status: status, ProductionID: credential.ProductionID, PerformanceStartsAt: credential.ValidPerformanceStartsAt, Message: message}, nil
}

func incompleteReviewItems(snapshot *domain.Snapshot) []string {
	latestReturn := ""
	for i := len(snapshot.Reviews) - 1; i >= 0; i-- {
		if snapshot.Reviews[i].Decision == "return" {
			latestReturn = snapshot.Reviews[i].ID
			break
		}
	}
	if latestReturn == "" {
		return nil
	}
	var gaps []string
	effectiveEvidence := snapshot.EffectiveEvidence()
	for _, item := range snapshot.ReviewItems {
		if item.ReviewID != latestReturn || item.Status == "closed" {
			continue
		}
		valid := item.Status == "responded"
		if item.RelatedRevision > 0 {
			valid = valid && item.RelatedRevision > reviewRevision(snapshot, latestReturn) && item.RelatedRevision <= snapshot.Production.Revision
		}
		if item.RelatedAnalysisRevision > 0 {
			valid = valid && item.RelatedAnalysisRevision == snapshot.Production.LastAnalysisRevision
		}
		if item.RelatedEvidenceID != "" {
			found := false
			for _, evidence := range effectiveEvidence {
				if evidence.ID == item.RelatedEvidenceID {
					found = true
				}
			}
			valid = valid && found
		}
		if item.RelatedRevision == 0 && item.RelatedAnalysisRevision == 0 && item.RelatedEvidenceID == "" {
			valid = false
		}
		if !valid {
			gaps = append(gaps, item.ID)
		}
	}
	return gaps
}

func newID(prefix string) string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "-fallback-" + time.Now().UTC().Format("20060102150405.000000000")
	}
	return prefix + "-" + hex.EncodeToString(raw[:])
}
