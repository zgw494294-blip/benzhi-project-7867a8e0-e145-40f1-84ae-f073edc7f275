package domain

import (
	"fmt"
	"sort"
	"strings"
)

const AnalysisRuleVersion = "stage-rigging-rules/1"

type FindingChange struct {
	Status         string             `json:"status"`
	FindingKey     string             `json:"findingKey"`
	RuleCode       string             `json:"ruleCode"`
	ElementIDs     []string           `json:"elementIDs"`
	CueIDs         []string           `json:"cueIDs"`
	Before         *SafetyFinding     `json:"before,omitempty"`
	After          *SafetyFinding     `json:"after,omitempty"`
	BeforeEvidence map[string]float64 `json:"beforeEvidence,omitempty"`
	AfterEvidence  map[string]float64 `json:"afterEvidence,omitempty"`
}

type AnalysisComparison struct {
	ProductionID string          `json:"productionID"`
	Older        AnalysisBatch   `json:"older"`
	Newer        AnalysisBatch   `json:"newer"`
	Changes      []FindingChange `json:"changes"`
}

type CueCoverage struct {
	CueID              string             `json:"cueID"`
	SequenceNo         int                `json:"sequenceNo"`
	Status             string             `json:"status"`
	MissingReason      string             `json:"missingReason,omitempty"`
	Evidence           *RehearsalEvidence `json:"evidence,omitempty"`
	ExpiredEvidenceIDs []string           `json:"expiredEvidenceIDs,omitempty"`
}

type EvidenceCoverage struct {
	ProductionID     string        `json:"productionID"`
	AnalysisRevision int64         `json:"analysisRevision"`
	Passed           int           `json:"passed"`
	Total            int           `json:"total"`
	Items            []CueCoverage `json:"items"`
}

func FindingIdentity(f SafetyFinding) string {
	elements := append([]string(nil), f.ElementIDs...)
	cues := append([]string(nil), f.CueIDs...)
	sort.Strings(elements)
	sort.Strings(cues)
	return f.RuleCode + "|" + strings.Join(elements, ",") + "|" + strings.Join(cues, ",")
}

func CompareAnalysis(older, newer AnalysisBatch) (AnalysisComparison, error) {
	if older.ProductionID == "" || newer.ProductionID == "" || older.ProductionID != newer.ProductionID {
		return AnalysisComparison{}, Invalid("analysisBatches", "分析批次必须属于同一制作")
	}
	if newer.AnalysisRevision <= older.AnalysisRevision {
		return AnalysisComparison{}, Invalid("analysisRevision", "较新分析批次的修订必须大于较旧批次")
	}
	before, after := map[string]SafetyFinding{}, map[string]SafetyFinding{}
	for _, finding := range older.Findings {
		before[FindingIdentity(finding)] = finding
	}
	for _, finding := range newer.Findings {
		after[FindingIdentity(finding)] = finding
	}
	keys := make([]string, 0, len(before)+len(after))
	seen := map[string]bool{}
	for key := range before {
		seen[key] = true
		keys = append(keys, key)
	}
	for key := range after {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	result := AnalysisComparison{ProductionID: older.ProductionID, Older: older, Newer: newer, Changes: make([]FindingChange, 0, len(keys))}
	for _, key := range keys {
		oldFinding, hadOld := before[key]
		newFinding, hasNew := after[key]
		change := FindingChange{FindingKey: key}
		switch {
		case hadOld && hasNew:
			change.Status, change.RuleCode = "persistent", newFinding.RuleCode
			change.ElementIDs, change.CueIDs = newFinding.ElementIDs, newFinding.CueIDs
			change.Before, change.After = &oldFinding, &newFinding
			change.BeforeEvidence, change.AfterEvidence = oldFinding.EvidenceValues, newFinding.EvidenceValues
		case hadOld:
			change.Status, change.RuleCode = "resolved", oldFinding.RuleCode
			change.ElementIDs, change.CueIDs = oldFinding.ElementIDs, oldFinding.CueIDs
			change.Before, change.BeforeEvidence = &oldFinding, oldFinding.EvidenceValues
		default:
			change.Status, change.RuleCode = "new", newFinding.RuleCode
			change.ElementIDs, change.CueIDs = newFinding.ElementIDs, newFinding.CueIDs
			change.After, change.AfterEvidence = &newFinding, newFinding.EvidenceValues
		}
		result.Changes = append(result.Changes, change)
	}
	return result, nil
}

func ValidateFinalPlan(production ProductionFile, elements []RiggingElement, cues []MotionCue) error {
	snapshot := Snapshot{Production: production, Elements: elements, Cues: cues}
	if err := snapshot.ValidateIntegrity(); err != nil {
		return err
	}
	byID := make(map[string]MotionCue, len(cues))
	for _, cue := range cues {
		byID[cue.ID] = cue
	}
	for _, cue := range cues {
		for _, prerequisiteID := range cue.PreconditionCueIDs {
			prerequisite := byID[prerequisiteID]
			if prerequisite.SequenceNo >= cue.SequenceNo {
				return Invalid("cues["+prerequisiteID+","+cue.ID+"]", fmt.Sprintf("前置提示 %s 的序号必须位于依赖提示 %s 之前", prerequisiteID, cue.ID))
			}
		}
	}
	return nil
}

func (s Snapshot) EffectiveEvidence() map[string]RehearsalEvidence {
	superseded := map[string]bool{}
	for _, evidence := range s.Evidence {
		if evidence.SupersedesEvidenceID != "" {
			superseded[evidence.SupersedesEvidenceID] = true
		}
	}
	latest := map[string]RehearsalEvidence{}
	for _, evidence := range s.Evidence {
		if evidence.AnalysisRevision != s.Production.LastAnalysisRevision || superseded[evidence.ID] {
			continue
		}
		current, exists := latest[evidence.CueID]
		if !exists || evidence.RunNumber > current.RunNumber {
			latest[evidence.CueID] = evidence
		}
	}
	return latest
}

func (s Snapshot) EvidenceCoverage() EvidenceCoverage {
	result := EvidenceCoverage{ProductionID: s.Production.ID, AnalysisRevision: s.Production.LastAnalysisRevision, Total: len(s.Cues), Items: make([]CueCoverage, 0, len(s.Cues))}
	latest := s.EffectiveEvidence()
	for _, cue := range s.Cues {
		item := CueCoverage{CueID: cue.ID, SequenceNo: cue.SequenceNo}
		for _, evidence := range s.Evidence {
			if evidence.CueID == cue.ID && evidence.AnalysisRevision != s.Production.LastAnalysisRevision {
				item.ExpiredEvidenceIDs = append(item.ExpiredEvidenceIDs, evidence.ID)
			}
		}
		if evidence, exists := latest[cue.ID]; exists {
			copy := evidence
			item.Evidence = &copy
			if evidence.Result == "passed" && (evidence.AttachmentName == "" || evidence.AttachmentSHA256 != "") {
				item.Status = "passed"
				result.Passed++
			} else if evidence.Result == "blocked" {
				item.Status, item.MissingReason = "blocked", "最新有效结果为阻断"
			} else {
				item.Status, item.MissingReason = "missing", "附件名称缺少有效摘要"
			}
		} else if len(item.ExpiredEvidenceIDs) > 0 {
			item.Status, item.MissingReason = "expired", "仅存在旧分析批次证据"
		} else {
			item.Status, item.MissingReason = "missing", "当前分析批次尚无排练证据"
		}
		result.Items = append(result.Items, item)
	}
	sort.Slice(result.Items, func(i, j int) bool { return result.Items[i].SequenceNo < result.Items[j].SequenceNo })
	return result
}

func (s Snapshot) EffectiveCredentialStatus(credentialID string) (string, string, string) {
	status, reason, replacement := "valid", "", ""
	for _, event := range s.CredentialEvents {
		if event.CredentialID == credentialID {
			status, reason, replacement = event.Status, event.Reason, event.ReplacementID
		}
	}
	return status, reason, replacement
}
