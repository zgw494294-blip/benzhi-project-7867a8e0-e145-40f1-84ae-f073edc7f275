package workflow

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"stageclearance/internal/domain"
)

type CloneProductionCommand struct {
	Title               string    `json:"title"`
	PerformanceStartsAt time.Time `json:"performanceStartsAt"`
	ActorID             string    `json:"actorID"`
	IdempotencyKey      string    `json:"idempotencyKey"`
}

type ElementChange struct {
	Operation string                 `json:"operation"`
	ElementID string                 `json:"elementID,omitempty"`
	Element   *domain.RiggingElement `json:"element,omitempty"`
}

type BatchElementsCommand struct {
	CommandMeta
	Changes []ElementChange `json:"changes"`
}

type CueScheduleChange struct {
	CueID      string `json:"cueID"`
	SequenceNo *int   `json:"sequenceNo,omitempty"`
}

type ScheduleCuesCommand struct {
	CommandMeta
	OffsetMS int64               `json:"offsetMS"`
	Changes  []CueScheduleChange `json:"changes"`
}

type ReviewResponseCommand struct {
	CommandMeta
	Response                string `json:"response"`
	RelatedRevision         int64  `json:"relatedRevision,omitempty"`
	RelatedAnalysisRevision int64  `json:"relatedAnalysisRevision,omitempty"`
	RelatedEvidenceID       string `json:"relatedEvidenceID,omitempty"`
}

type RevokeCredentialCommand struct {
	CommandMeta
	Reason string `json:"reason"`
}

type ReissueCredentialCommand struct {
	CommandMeta
	Reason string `json:"reason"`
}

func (s *Service) CloneProduction(ctx context.Context, sourceID string, command CloneProductionCommand) (domain.Snapshot, error) {
	return s.store.CloneCreate(ctx, sourceID, command.IdempotencyKey, command.ActorID, func(source domain.Snapshot) (domain.Snapshot, error) {
		if command.ActorID == "" || command.ActorID != source.Production.TechnicalDirectorID {
			return domain.Snapshot{}, domain.Invalid("actorID", "只有来源档案的技术总监可以复制方案")
		}
		if err := domain.ValidateFinalPlan(source.Production, source.Elements, source.Cues); err != nil {
			return domain.Snapshot{}, domain.Invalid("source."+domainErrorField(err), "来源方案未通过当前领域规则校验："+err.Error())
		}
		now := s.now()
		production := domain.ProductionFile{
			ID: newID("prod"), Title: strings.TrimSpace(command.Title), VenueName: source.Production.VenueName,
			StageWidthMM: source.Production.StageWidthMM, StageDepthMM: source.Production.StageDepthMM,
			PerformanceStartsAt: command.PerformanceStartsAt.UTC(), TechnicalDirectorID: command.ActorID,
			State: domain.StateDraft, Revision: 1, LastEditorID: command.ActorID, CreatedAt: now, UpdatedAt: now,
		}
		if err := domain.ValidateProduction(production); err != nil {
			return domain.Snapshot{}, err
		}
		elementIDs := map[string]string{}
		for _, element := range source.Elements {
			elementIDs[element.ID] = newID("element")
		}
		cueIDs := map[string]string{}
		for _, cue := range source.Cues {
			cueIDs[cue.ID] = newID("cue")
		}
		created := domain.Snapshot{Production: production, Elements: make([]domain.RiggingElement, 0, len(source.Elements)), Cues: make([]domain.MotionCue, 0, len(source.Cues)), Findings: []domain.SafetyFinding{}, Evidence: []domain.RehearsalEvidence{}, Reviews: []domain.ReviewDecision{}, Credentials: []domain.ReleaseCredential{}, AnalysisBatches: []domain.AnalysisBatch{}, Remediations: []domain.FindingRemediation{}, ReviewItems: []domain.ReviewResponseItem{}, CredentialEvents: []domain.CredentialStatusEvent{}}
		for _, old := range source.Elements {
			item := old
			item.ID, item.ProductionID, item.Revision = elementIDs[old.ID], production.ID, 1
			if old.ParentElementID != "" {
				mapped, exists := elementIDs[old.ParentElementID]
				if !exists {
					return domain.Snapshot{}, domain.Invalid("source.elements.parentElementID", "来源连接上级无法重映射")
				}
				item.ParentElementID = mapped
			}
			created.Elements = append(created.Elements, item)
		}
		for _, old := range source.Cues {
			item := old
			item.ID, item.ProductionID, item.Revision = cueIDs[old.ID], production.ID, 1
			mappedElement, exists := elementIDs[old.ElementID]
			if !exists {
				return domain.Snapshot{}, domain.Invalid("source.cues.elementID", "来源动作设备无法重映射")
			}
			item.ElementID = mappedElement
			item.PreconditionCueIDs = make([]string, 0, len(old.PreconditionCueIDs))
			for _, prerequisite := range old.PreconditionCueIDs {
				mapped, exists := cueIDs[prerequisite]
				if !exists {
					return domain.Snapshot{}, domain.Invalid("source.cues.preconditionCueIDs", "来源互锁前置关系无法重映射")
				}
				item.PreconditionCueIDs = append(item.PreconditionCueIDs, mapped)
			}
			created.Cues = append(created.Cues, item)
		}
		if err := domain.ValidateFinalPlan(created.Production, created.Elements, created.Cues); err != nil {
			return domain.Snapshot{}, err
		}
		return created, nil
	})
}

func domainErrorField(err error) string {
	if item, ok := err.(*domain.Error); ok {
		return item.Field
	}
	return "plan"
}

func (s *Service) BatchElements(ctx context.Context, productionID string, command BatchElementsCommand) (domain.Snapshot, error) {
	summary := make([]string, 0, len(command.Changes))
	for _, change := range command.Changes {
		target := change.ElementID
		if target == "" && change.Element != nil {
			target = change.Element.ID
			if target == "" {
				target = change.Element.Label
			}
		}
		summary = append(summary, change.Operation+":"+target)
	}
	detail := fmt.Sprintf("原子变更 %d 项吊挂元素：%s", len(command.Changes), strings.Join(summary, ","))
	return s.store.UpdateDetailed(ctx, productionID, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "elements.batch", detail, func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "批量变更吊挂元素")
		}
		if strings.TrimSpace(command.ActorID) == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		if len(command.Changes) == 0 {
			return domain.Invalid("changes", "批量变更不能为空")
		}
		items := make(map[string]domain.RiggingElement, len(snapshot.Elements))
		for _, element := range snapshot.Elements {
			items[element.ID] = element
		}
		targets := map[string]bool{}
		for index, change := range command.Changes {
			target := change.ElementID
			if change.Operation == "add" && change.Element != nil {
				target = change.Element.ID
				if target == "" {
					target = newID("element")
				}
			}
			if target == "" {
				return domain.Invalid(fmt.Sprintf("changes[%d].elementID", index), "操作目标不能为空")
			}
			if targets[target] {
				return domain.Invalid(fmt.Sprintf("changes[%d].elementID", index), "批次内元素标识及操作目标不能重复")
			}
			targets[target] = true
			switch change.Operation {
			case "add":
				if change.Element == nil {
					return domain.Invalid(fmt.Sprintf("changes[%d].element", index), "新增操作必须提供元素")
				}
				if _, exists := items[target]; exists {
					return domain.Invalid(fmt.Sprintf("changes[%d].elementID", index), "吊挂元素标识已存在")
				}
				item := *change.Element
				item.ID, item.ProductionID, item.Revision = target, productionID, command.ExpectedRevision+1
				items[target] = item
			case "update":
				if change.Element == nil {
					return domain.Invalid(fmt.Sprintf("changes[%d].element", index), "修改操作必须提供元素")
				}
				if _, exists := items[target]; !exists {
					return domain.NotFound("element", target)
				}
				item := *change.Element
				item.ID, item.ProductionID, item.Revision = target, productionID, command.ExpectedRevision+1
				items[target] = item
			case "delete":
				if _, exists := items[target]; !exists {
					return domain.NotFound("element", target)
				}
				delete(items, target)
			default:
				return domain.Invalid(fmt.Sprintf("changes[%d].operation", index), "操作必须为 add、update 或 delete")
			}
		}
		for _, element := range items {
			if element.ParentElementID != "" {
				if _, exists := items[element.ParentElementID]; !exists {
					return domain.Invalid("elements["+element.ID+"].parentElementID", "元素 "+element.ID+" 引用已删除或不存在的上级 "+element.ParentElementID)
				}
			}
		}
		for _, cue := range snapshot.Cues {
			if _, exists := items[cue.ElementID]; !exists {
				return domain.Invalid("cues["+cue.ID+"].elementID", "动作提示 "+cue.ID+" 仍引用已删除元素 "+cue.ElementID)
			}
		}
		final := make([]domain.RiggingElement, 0, len(items))
		for _, element := range items {
			final = append(final, element)
		}
		sort.Slice(final, func(i, j int) bool { return final[i].ID < final[j].ID })
		if err := domain.ValidateFinalPlan(snapshot.Production, final, snapshot.Cues); err != nil {
			return err
		}
		snapshot.Elements = final
		markPlanEdited(snapshot, command.ActorID)
		return nil
	})
}

func (s *Service) ScheduleCues(ctx context.Context, productionID string, command ScheduleCuesCommand) (domain.Snapshot, error) {
	cueIDs := make([]string, 0, len(command.Changes))
	for _, change := range command.Changes {
		cueIDs = append(cueIDs, change.CueID)
	}
	detail := fmt.Sprintf("调度 %d 项提示 [%s]，时间偏移 %dms", len(command.Changes), strings.Join(cueIDs, ","), command.OffsetMS)
	return s.store.UpdateDetailed(ctx, productionID, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "cues.schedule", detail, func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "批量调度动作提示")
		}
		if strings.TrimSpace(command.ActorID) == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		if len(command.Changes) == 0 {
			return domain.Invalid("changes", "至少选择一个动作提示")
		}
		indexes, seen := map[string]int{}, map[string]bool{}
		for index, cue := range snapshot.Cues {
			indexes[cue.ID] = index
		}
		final := append([]domain.MotionCue(nil), snapshot.Cues...)
		for index, change := range command.Changes {
			if seen[change.CueID] {
				return domain.Invalid(fmt.Sprintf("changes[%d].cueID", index), "批次内提示标识不能重复")
			}
			seen[change.CueID] = true
			position, exists := indexes[change.CueID]
			if !exists {
				return domain.NotFound("cue", change.CueID)
			}
			final[position].StartsAtMS += command.OffsetMS
			if change.SequenceNo != nil {
				final[position].SequenceNo = *change.SequenceNo
			}
			final[position].Revision = command.ExpectedRevision + 1
		}
		if err := domain.ValidateFinalPlan(snapshot.Production, snapshot.Elements, final); err != nil {
			return err
		}
		snapshot.Cues = final
		markPlanEdited(snapshot, command.ActorID)
		return nil
	})
}

func (s *Service) AnalysisHistory(ctx context.Context, productionID string) ([]domain.AnalysisBatch, error) {
	snapshot, err := s.store.Get(ctx, productionID)
	if err != nil {
		return nil, err
	}
	result := append([]domain.AnalysisBatch(nil), snapshot.AnalysisBatches...)
	sort.Slice(result, func(i, j int) bool { return result[i].AnalysisRevision < result[j].AnalysisRevision })
	return result, nil
}

func (s *Service) CompareAnalysis(ctx context.Context, productionID string, olderRevision, newerRevision int64) (domain.AnalysisComparison, error) {
	history, err := s.AnalysisHistory(ctx, productionID)
	if err != nil {
		return domain.AnalysisComparison{}, err
	}
	var older, newer *domain.AnalysisBatch
	for i := range history {
		if history[i].AnalysisRevision == olderRevision {
			older = &history[i]
		}
		if history[i].AnalysisRevision == newerRevision {
			newer = &history[i]
		}
	}
	if older == nil {
		return domain.AnalysisComparison{}, domain.NotFound("olderAnalysisRevision", fmt.Sprint(olderRevision))
	}
	if newer == nil {
		return domain.AnalysisComparison{}, domain.NotFound("newerAnalysisRevision", fmt.Sprint(newerRevision))
	}
	return domain.CompareAnalysis(*older, *newer)
}

func (s *Service) EvidenceCoverage(ctx context.Context, productionID string) (domain.EvidenceCoverage, error) {
	snapshot, err := s.store.Get(ctx, productionID)
	if err != nil {
		return domain.EvidenceCoverage{}, err
	}
	return snapshot.EvidenceCoverage(), nil
}

func (s *Service) RespondReviewItem(ctx context.Context, productionID, itemID string, command ReviewResponseCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, productionID, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "review.response", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StateRemediation && snapshot.Production.State != domain.StatePendingRehearsal {
			return domain.StateConflict(snapshot.Production.State, "回应退回复核意见")
		}
		if strings.TrimSpace(command.Response) == "" {
			return domain.Invalid("response", "回应内容不能为空")
		}
		index := -1
		for i := range snapshot.ReviewItems {
			if snapshot.ReviewItems[i].ID == itemID {
				index = i
				break
			}
		}
		if index < 0 {
			return domain.NotFound("reviewItem", itemID)
		}
		item := snapshot.ReviewItems[index]
		if item.Status == "closed" {
			return domain.Invalid("reviewItem", "已关闭的历史意见不能重新回应")
		}
		validReference := false
		if command.RelatedRevision > 0 {
			if command.RelatedRevision <= reviewRevision(snapshot, item.ReviewID) || command.RelatedRevision > snapshot.Production.Revision {
				return domain.Invalid("relatedRevision", "关联方案修订必须在退回之后且当前可见")
			}
			validReference = true
		}
		if command.RelatedAnalysisRevision > 0 {
			if command.RelatedAnalysisRevision <= reviewRevision(snapshot, item.ReviewID) || command.RelatedAnalysisRevision != snapshot.Production.LastAnalysisRevision {
				return domain.Invalid("relatedAnalysisRevision", "关联分析批次必须在退回之后且为当前有效批次")
			}
			validReference = true
		}
		if command.RelatedEvidenceID != "" {
			evidenceValid := false
			for _, evidence := range snapshot.EffectiveEvidence() {
				if evidence.ID == command.RelatedEvidenceID {
					evidenceValid = true
					break
				}
			}
			if !evidenceValid {
				return domain.Invalid("relatedEvidenceID", "关联证据不存在、已过期或已被替代")
			}
			validReference = true
		}
		if !validReference {
			return domain.Invalid("reference", "回应必须关联退回后产生的方案修订、分析批次或有效排练证据之一")
		}
		item.Response, item.RespondedBy, item.RespondedAt, item.Status = strings.TrimSpace(command.Response), command.ActorID, s.now(), "responded"
		item.RelatedRevision, item.RelatedAnalysisRevision, item.RelatedEvidenceID = command.RelatedRevision, command.RelatedAnalysisRevision, command.RelatedEvidenceID
		snapshot.ReviewItems[index] = item
		return nil
	})
}

func reviewRevision(snapshot *domain.Snapshot, reviewID string) int64 {
	for _, review := range snapshot.Reviews {
		if review.ID == reviewID {
			return review.ProductionRevision
		}
	}
	return 0
}

func (s *Service) RevokeCredential(ctx context.Context, productionID, credentialID string, command RevokeCredentialCommand) (domain.Snapshot, error) {
	result, err := s.store.Update(ctx, productionID, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "release.revoke", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StateReleased {
			return domain.StateConflict(snapshot.Production.State, "作废放行凭据")
		}
		if command.ActorID != snapshot.Production.TechnicalDirectorID {
			return domain.Invalid("actorID", "只有技术总监可以作废凭据")
		}
		if strings.TrimSpace(command.Reason) == "" {
			return domain.Invalid("reason", "作废原因不能为空")
		}
		credential, exists := credentialByID(snapshot, credentialID)
		if !exists {
			return domain.NotFound("credential", credentialID)
		}
		status, _, _ := snapshot.EffectiveCredentialStatus(credential.ID)
		if status != "valid" {
			return domain.Invalid("credentialID", "凭据当前不是有效状态")
		}
		snapshot.CredentialEvents = append(snapshot.CredentialEvents, domain.CredentialStatusEvent{ID: newID("credential-event"), CredentialID: credentialID, Status: "revoked", Reason: strings.TrimSpace(command.Reason), ActorID: command.ActorID, CreatedAt: s.now()})
		return nil
	})
	if err == nil {
		s.invalidateCredentialVerification(result, credentialID)
	}
	return result, err
}

func (s *Service) ReissueCredential(ctx context.Context, productionID, credentialID string, command ReissueCredentialCommand) (domain.Snapshot, error) {
	result, err := s.store.Update(ctx, productionID, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "release.reissue", func(snapshot *domain.Snapshot) error {
		if snapshot.Production.State != domain.StateReleased {
			return domain.StateConflict(snapshot.Production.State, "换发放行凭据")
		}
		if command.ActorID != snapshot.Production.TechnicalDirectorID {
			return domain.Invalid("actorID", "只有技术总监可以换发凭据")
		}
		old, exists := credentialByID(snapshot, credentialID)
		if !exists {
			return domain.NotFound("credential", credentialID)
		}
		if snapshot.Manifest == nil || snapshot.Manifest.SHA256 != old.ManifestSHA256 || snapshot.Manifest.Revision != old.FrozenRevision {
			return domain.Invalid("manifest", "冻结摘要与原凭据不匹配")
		}
		matches, err := domain.VerifyManifest(*snapshot, *snapshot.Manifest)
		if err != nil || !matches {
			return domain.Invalid("manifest", "冻结内容校验不匹配")
		}
		if !old.ValidPerformanceStartsAt.Equal(snapshot.Production.PerformanceStartsAt) {
			return domain.Invalid("performanceStartsAt", "原凭据场次与制作档案不一致")
		}
		approved := false
		for _, review := range snapshot.Reviews {
			if review.Decision == "approve" && review.ReviewerID == old.ApprovedBy {
				approved = true
			}
		}
		if !approved {
			return domain.Invalid("approval", "档案没有与原凭据匹配的有效批准记录")
		}
		for _, current := range snapshot.Credentials {
			status, _, _ := snapshot.EffectiveCredentialStatus(current.ID)
			if status == "valid" && current.ID != old.ID {
				return domain.Invalid("credentials", "同一冻结清单已有当前有效凭据")
			}
		}
		newCredential := old
		newCredential.ID, newCredential.IssuedAt, newCredential.IssuedBy, newCredential.ReplacesCredentialID = newID("credential"), s.now(), command.ActorID, old.ID
		newCredential.VerificationCode = domain.CredentialCodeForIssue(newCredential.ManifestSHA256, productionID, newCredential.FrozenRevision, newCredential.ValidPerformanceStartsAt, newCredential.ID)
		if newCredential.VerificationCode == old.VerificationCode {
			return domain.Invalid("verificationCode", "换发校验码必须与原码不同")
		}
		reason := strings.TrimSpace(command.Reason)
		if reason == "" {
			reason = "受控换发"
		}
		snapshot.CredentialEvents = append(snapshot.CredentialEvents, domain.CredentialStatusEvent{ID: newID("credential-event"), CredentialID: old.ID, Status: "replaced", Reason: reason, ActorID: command.ActorID, ReplacementID: newCredential.ID, CreatedAt: s.now()})
		snapshot.Credentials = append(snapshot.Credentials, newCredential)
		return nil
	})
	if err == nil {
		s.invalidateCredentialVerification(result, credentialID)
	}
	return result, err
}

func (s *Service) invalidateCredentialVerification(snapshot domain.Snapshot, credentialID string) {
	for _, credential := range snapshot.Credentials {
		if credential.ID == credentialID {
			delete(s.verificationCache, credential.VerificationCode)
			return
		}
	}
}

func credentialByID(snapshot *domain.Snapshot, id string) (domain.ReleaseCredential, bool) {
	for _, credential := range snapshot.Credentials {
		if credential.ID == id {
			return credential, true
		}
	}
	return domain.ReleaseCredential{}, false
}

func markPlanEdited(snapshot *domain.Snapshot, actorID string) {
	snapshot.Production.LastEditorID = actorID
	for i := range snapshot.Remediations {
		if snapshot.Remediations[i].Status == "awaiting_rehearsal" {
			snapshot.Remediations[i].Status, snapshot.Remediations[i].ResolvedAnalysisRevision = "in_progress", 0
		}
	}
}
