package analyzer

import (
	"fmt"
	"math"

	"stageclearance/internal/domain"
)

func operationalFindings(snapshot domain.Snapshot) []domain.SafetyFinding {
	var findings []domain.SafetyFinding
	elements := make(map[string]domain.RiggingElement)
	for _, element := range snapshot.Elements {
		elements[element.ID] = element
	}
	for _, cue := range snapshot.Cues {
		element, exists := elements[cue.ElementID]
		if !exists {
			findings = append(findings, domain.SafetyFinding{
				RuleCode: "REF-001", Severity: "blocking", CueIDs: []string{cue.ID},
				Explanation:    fmt.Sprintf("提示 %d 引用的吊挂设备 %s 不存在", cue.SequenceNo, cue.ElementID),
				EvidenceValues: map[string]float64{"missingElement": 1},
			})
			continue
		}
		distance := math.Abs(element.ZMM - cue.TargetZMM)
		availableSeconds := float64(cue.DurationMS) / 1000
		requiredSeconds := distance / cue.SpeedMMPerSecond
		if requiredSeconds > availableSeconds {
			findings = append(findings, domain.SafetyFinding{
				RuleCode: "RATE-001", Severity: "blocking", ElementIDs: []string{element.ID}, CueIDs: []string{cue.ID},
				Explanation:    fmt.Sprintf("提示 %d 按 %.0fmm/s 移动 %.0fmm 至少需要 %.2fs，时间窗仅 %.2fs", cue.SequenceNo, cue.SpeedMMPerSecond, distance, requiredSeconds, availableSeconds),
				EvidenceValues: map[string]float64{"distanceMM": distance, "requiredSeconds": requiredSeconds, "availableSeconds": availableSeconds, "speedMMPerSecond": cue.SpeedMMPerSecond},
			})
		}
	}
	return findings
}

func prerequisiteTimingFindings(snapshot domain.Snapshot) []domain.SafetyFinding {
	byID := make(map[string]domain.MotionCue)
	for _, cue := range snapshot.Cues {
		byID[cue.ID] = cue
	}
	var findings []domain.SafetyFinding
	for _, cue := range snapshot.Cues {
		for _, dependencyID := range cue.PreconditionCueIDs {
			dependency, exists := byID[dependencyID]
			if !exists {
				continue
			}
			dependencyEnds := dependency.StartsAtMS + dependency.DurationMS
			if dependencyEnds > cue.StartsAtMS {
				findings = append(findings, domain.SafetyFinding{
					RuleCode: "LOCK-003", Severity: "blocking", CueIDs: []string{dependency.ID, cue.ID},
					Explanation:    fmt.Sprintf("提示 %d 在前置提示 %d 完成前 %dms 已开始，互锁条件不可达", cue.SequenceNo, dependency.SequenceNo, dependencyEnds-cue.StartsAtMS),
					EvidenceValues: map[string]float64{"preconditionEndMS": float64(dependencyEnds), "dependentStartMS": float64(cue.StartsAtMS)},
				})
			}
		}
	}
	return findings
}

func connectionFindings(snapshot domain.Snapshot) []domain.SafetyFinding {
	byID := make(map[string]domain.RiggingElement)
	for _, element := range snapshot.Elements {
		byID[element.ID] = element
	}
	var findings []domain.SafetyFinding
	for _, element := range snapshot.Elements {
		if element.ParentElementID == "" {
			continue
		}
		parent, exists := byID[element.ParentElementID]
		if !exists {
			findings = append(findings, domain.SafetyFinding{
				RuleCode: "REF-002", Severity: "blocking", ElementIDs: []string{element.ID, element.ParentElementID},
				Explanation:    fmt.Sprintf("%s 连接的上级元素 %s 不存在", element.Label, element.ParentElementID),
				EvidenceValues: map[string]float64{"missingParent": 1},
			})
			continue
		}
		if element.RatedLoadKG > parent.RatedLoadKG {
			findings = append(findings, domain.SafetyFinding{
				RuleCode: "CONN-001", Severity: "warning", ElementIDs: []string{parent.ID, element.ID},
				Explanation:    fmt.Sprintf("下级 %s 的额定载荷 %.2fkg 高于上级 %s 的 %.2fkg，连接链能力由上级限制", element.Label, element.RatedLoadKG, parent.Label, parent.RatedLoadKG),
				EvidenceValues: map[string]float64{"childRatedLoadKG": element.RatedLoadKG, "parentRatedLoadKG": parent.RatedLoadKG},
			})
		}
	}
	return findings
}
