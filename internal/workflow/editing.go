package workflow

import (
	"context"
	"strings"
	"time"

	"stageclearance/internal/domain"
)

type UpdateProductionCommand struct {
	CommandMeta
	Title               string    `json:"title"`
	VenueName           string    `json:"venueName"`
	StageWidthMM        float64   `json:"stageWidthMM"`
	StageDepthMM        float64   `json:"stageDepthMM"`
	PerformanceStartsAt time.Time `json:"performanceStartsAt"`
}

func (s *Service) UpdateProduction(ctx context.Context, id string, command UpdateProductionCommand) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "production.update", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "修改制作档案")
		}
		if command.ActorID == "" {
			return domain.Invalid("actorID", "编辑者不能为空")
		}
		updated := snapshot.Production
		updated.Title = strings.TrimSpace(command.Title)
		updated.VenueName = strings.TrimSpace(command.VenueName)
		updated.StageWidthMM = command.StageWidthMM
		updated.StageDepthMM = command.StageDepthMM
		updated.PerformanceStartsAt = command.PerformanceStartsAt.UTC()
		if err := domain.ValidateProduction(updated); err != nil {
			return err
		}
		for _, element := range snapshot.Elements {
			if element.XMM > updated.StageWidthMM || element.YMM > updated.StageDepthMM {
				return domain.Invalid("stageBounds", "缩小后的舞台边界不能排除已有吊挂元素")
			}
		}
		snapshot.Production = updated
		markPlanEdited(snapshot, command.ActorID)
		return nil
	})
}

func (s *Service) RemoveElement(ctx context.Context, id, elementID string, command CommandMeta) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "element.remove", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "删除吊挂元素")
		}
		index := -1
		for i, element := range snapshot.Elements {
			if element.ID == elementID {
				index = i
			}
			if element.ParentElementID == elementID {
				return domain.Invalid("elementID", "仍有吊挂元素连接到该元素")
			}
		}
		if index < 0 {
			return domain.NotFound("element", elementID)
		}
		for _, cue := range snapshot.Cues {
			if cue.ElementID == elementID {
				return domain.Invalid("elementID", "仍有动作提示引用该元素")
			}
		}
		snapshot.Elements = append(snapshot.Elements[:index], snapshot.Elements[index+1:]...)
		markPlanEdited(snapshot, command.ActorID)
		return nil
	})
}

func (s *Service) RemoveCue(ctx context.Context, id, cueID string, command CommandMeta) (domain.Snapshot, error) {
	return s.store.Update(ctx, id, command.ExpectedRevision, command.IdempotencyKey, command.ActorID, "cue.remove", func(snapshot *domain.Snapshot) error {
		if !domain.CanEdit(snapshot.Production.State) {
			return domain.StateConflict(snapshot.Production.State, "删除动作提示")
		}
		index := -1
		for i, cue := range snapshot.Cues {
			if cue.ID == cueID {
				index = i
			}
			for _, dependency := range cue.PreconditionCueIDs {
				if dependency == cueID {
					return domain.Invalid("cueID", "仍有动作提示依赖该前置提示")
				}
			}
		}
		if index < 0 {
			return domain.NotFound("cue", cueID)
		}
		snapshot.Cues = append(snapshot.Cues[:index], snapshot.Cues[index+1:]...)
		markPlanEdited(snapshot, command.ActorID)
		return nil
	})
}
