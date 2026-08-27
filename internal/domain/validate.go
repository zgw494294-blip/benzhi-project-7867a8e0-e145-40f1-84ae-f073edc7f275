package domain

import (
	"encoding/hex"
	"strings"
)

func ValidateProduction(p ProductionFile) error {
	if strings.TrimSpace(p.Title) == "" {
		return Invalid("title", "制作名称不能为空")
	}
	if strings.TrimSpace(p.VenueName) == "" {
		return Invalid("venueName", "场地名称不能为空")
	}
	if p.StageWidthMM <= 0 {
		return Invalid("stageWidthMM", "舞台宽度必须大于零")
	}
	if p.StageDepthMM <= 0 {
		return Invalid("stageDepthMM", "舞台深度必须大于零")
	}
	if p.PerformanceStartsAt.IsZero() {
		return Invalid("performanceStartsAt", "演出场次时间不能为空")
	}
	if strings.TrimSpace(p.TechnicalDirectorID) == "" {
		return Invalid("technicalDirectorID", "技术总监不能为空")
	}
	return nil
}

func ValidateElement(e RiggingElement, p ProductionFile, elements []RiggingElement) error {
	if strings.TrimSpace(e.Label) == "" {
		return Invalid("label", "吊挂元素名称不能为空")
	}
	switch e.Kind {
	case "point", "batten", "hoist", "load":
	default:
		return Invalid("kind", "元素类型必须为 point、batten、hoist 或 load")
	}
	if e.XMM < 0 || e.XMM > p.StageWidthMM || e.YMM < 0 || e.YMM > p.StageDepthMM || e.ZMM < 0 {
		return Invalid("coordinates", "元素坐标必须位于舞台边界内")
	}
	if e.RatedLoadKG <= 0 {
		return Invalid("ratedLoadKG", "额定载荷必须大于零")
	}
	if e.AppliedLoadKG < 0 {
		return Invalid("appliedLoadKG", "实际载荷不能为负数")
	}
	if e.SafetyFactor < 1 {
		return Invalid("safetyFactor", "安全系数不能小于 1")
	}
	if e.ParentElementID != "" {
		if e.ParentElementID == e.ID {
			return Invalid("parentElementID", "元素不能连接自身")
		}
		found := false
		parentByID := map[string]string{}
		for _, item := range elements {
			parentByID[item.ID] = item.ParentElementID
			if item.ID == e.ParentElementID {
				found = true
			}
		}
		if !found {
			return Invalid("parentElementID", "上级吊挂元素不存在")
		}
		for id := e.ParentElementID; id != ""; id = parentByID[id] {
			if id == e.ID {
				return Invalid("parentElementID", "连接关系不能形成循环")
			}
		}
	}
	return nil
}

func ValidateCue(c MotionCue, elements []RiggingElement, cues []MotionCue) error {
	if c.SequenceNo <= 0 {
		return Invalid("sequenceNo", "提示序号必须大于零")
	}
	if c.StartsAtMS < 0 || c.DurationMS <= 0 {
		return Invalid("timeWindow", "开始时间不能为负且持续时间必须大于零")
	}
	if c.TargetZMM < 0 || c.SpeedMMPerSecond <= 0 {
		return Invalid("motion", "目标高度不能为负且速度必须大于零")
	}
	if strings.TrimSpace(c.OperatorGroup) == "" {
		return Invalid("operatorGroup", "操作组不能为空")
	}
	elementFound := false
	for _, e := range elements {
		if e.ID == c.ElementID {
			elementFound = true
			break
		}
	}
	if !elementFound {
		return Invalid("elementID", "动作提示引用的设备不存在")
	}
	known := map[string]bool{}
	graph := map[string][]string{}
	for _, item := range cues {
		known[item.ID] = true
		graph[item.ID] = item.PreconditionCueIDs
	}
	known[c.ID] = true
	graph[c.ID] = c.PreconditionCueIDs
	for _, id := range c.PreconditionCueIDs {
		if !known[id] {
			return Invalid("preconditionCueIDs", "互锁前置提示不存在")
		}
		if id == c.ID {
			return Invalid("preconditionCueIDs", "提示不能依赖自身")
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, next := range graph[id] {
			if visit(next) {
				return true
			}
		}
		visiting[id] = false
		visited[id] = true
		return false
	}
	for id := range graph {
		if visit(id) {
			return Invalid("preconditionCueIDs", "提示互锁不能形成循环")
		}
	}
	return nil
}

func ValidateEvidence(e RehearsalEvidence, cues []MotionCue) error {
	if e.RunNumber <= 0 {
		return Invalid("runNumber", "排练轮次必须大于零")
	}
	if e.PerformedAt.IsZero() {
		return Invalid("performedAt", "执行时间不能为空")
	}
	if strings.TrimSpace(e.OperatorID) == "" {
		return Invalid("operatorID", "操作员不能为空")
	}
	if e.Result != "passed" && e.Result != "blocked" {
		return Invalid("result", "排练结果必须为 passed 或 blocked")
	}
	if e.MeasuredDeviationMM < 0 {
		return Invalid("measuredDeviationMM", "实测偏差不能为负")
	}
	found := false
	for _, c := range cues {
		if c.ID == e.CueID {
			found = true
			break
		}
	}
	if !found {
		return Invalid("cueID", "排练提示不存在")
	}
	if e.AttachmentName != "" {
		decoded, err := hex.DecodeString(e.AttachmentSHA256)
		if err != nil || len(decoded) != 32 {
			return Invalid("attachmentSHA256", "附件摘要必须是 64 位 SHA-256 十六进制值")
		}
	}
	if e.Result == "blocked" && strings.TrimSpace(e.IncidentDescription) == "" {
		return Invalid("incidentDescription", "阻断结果必须说明现场事件")
	}
	return nil
}

func CanEdit(state ProductionState) bool { return state == StateDraft || state == StateRemediation }

func CanTransition(from, to ProductionState) bool {
	allowed := map[ProductionState]map[ProductionState]bool{
		StateDraft: {StatePendingAnalysis: true}, StateRemediation: {StatePendingAnalysis: true},
		StatePendingAnalysis:  {StatePendingRehearsal: true, StateRemediation: true},
		StatePendingRehearsal: {StatePendingReview: true, StateRemediation: true},
		StatePendingReview:    {StateFrozen: true, StateRemediation: true}, StateFrozen: {StateReleased: true},
	}
	return allowed[from][to]
}
