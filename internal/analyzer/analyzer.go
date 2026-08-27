package analyzer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"

	"stageclearance/internal/domain"
)

type Analyzer struct{ MinimumClearanceMM float64 }

func New() *Analyzer { return &Analyzer{MinimumClearanceMM: 500} }

func (a *Analyzer) Analyze(s domain.Snapshot) []domain.SafetyFinding {
	var findings []domain.SafetyFinding
	findings = append(findings, a.loadFindings(s)...)
	findings = append(findings, a.spatialFindings(s)...)
	findings = append(findings, a.timingFindings(s)...)
	findings = append(findings, dependencyFindings(s)...)
	findings = append(findings, operationalFindings(s)...)
	findings = append(findings, prerequisiteTimingFindings(s)...)
	findings = append(findings, connectionFindings(s)...)
	for i := range findings {
		findings[i].ProductionID = s.Production.ID
		findings[i].AnalysisRevision = s.Production.Revision
		findings[i].ResolutionState = "open"
		findings[i].ID = findingID(findings[i])
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].RuleCode != findings[j].RuleCode {
			return findings[i].RuleCode < findings[j].RuleCode
		}
		return findings[i].ID < findings[j].ID
	})
	return findings
}

func findingID(f domain.SafetyFinding) string {
	payload := fmt.Sprintf("%s|%v|%v|%s", f.RuleCode, f.ElementIDs, f.CueIDs, f.Explanation)
	sum := sha256.Sum256([]byte(payload))
	return "finding-" + hex.EncodeToString(sum[:])[:12]
}

func (a *Analyzer) loadFindings(s domain.Snapshot) []domain.SafetyFinding {
	var out []domain.SafetyFinding
	children := map[string]float64{}
	for _, e := range s.Elements {
		if e.ParentElementID != "" {
			children[e.ParentElementID] += e.AppliedLoadKG
		}
	}
	for _, e := range s.Elements {
		total := e.AppliedLoadKG + children[e.ID]
		permitted := e.RatedLoadKG / e.SafetyFactor
		if total > permitted {
			out = append(out, domain.SafetyFinding{RuleCode: "LOAD-001", Severity: "blocking", ElementIDs: []string{e.ID}, Explanation: fmt.Sprintf("%s 承受 %.2fkg，按安全系数 %.2f 的允许载荷为 %.2fkg", e.Label, total, e.SafetyFactor, permitted), EvidenceValues: map[string]float64{"totalLoadKG": total, "permittedLoadKG": permitted, "ratedLoadKG": e.RatedLoadKG, "safetyFactor": e.SafetyFactor}})
		}
		if e.SafetyFactor < 1.5 {
			out = append(out, domain.SafetyFinding{RuleCode: "LOAD-002", Severity: "warning", ElementIDs: []string{e.ID}, Explanation: fmt.Sprintf("%s 的安全系数 %.2f 低于建议值 1.50", e.Label, e.SafetyFactor), EvidenceValues: map[string]float64{"safetyFactor": e.SafetyFactor, "recommended": 1.5}})
		}
	}
	return out
}

func (a *Analyzer) spatialFindings(s domain.Snapshot) []domain.SafetyFinding {
	var out []domain.SafetyFinding
	elements := map[string]domain.RiggingElement{}
	for _, e := range s.Elements {
		elements[e.ID] = e
	}
	for _, cue := range s.Cues {
		e, ok := elements[cue.ElementID]
		if !ok {
			continue
		}
		clearance := cue.MinimumClearanceMM
		if clearance == 0 {
			clearance = cue.TargetZMM
		}
		if clearance < a.MinimumClearanceMM {
			out = append(out, domain.SafetyFinding{RuleCode: "SPACE-001", Severity: "blocking", ElementIDs: []string{e.ID}, CueIDs: []string{cue.ID}, Explanation: fmt.Sprintf("提示 %d 的最小净空 %.0fmm 低于要求 %.0fmm", cue.SequenceNo, clearance, a.MinimumClearanceMM), EvidenceValues: map[string]float64{"clearanceMM": clearance, "requiredMM": a.MinimumClearanceMM}})
		}
		if cue.ExclusionZone && cue.PersonnelInZone {
			out = append(out, domain.SafetyFinding{RuleCode: "ZONE-001", Severity: "blocking", ElementIDs: []string{e.ID}, CueIDs: []string{cue.ID}, Explanation: fmt.Sprintf("提示 %d 的动作路径穿越仍有人员的禁入区", cue.SequenceNo), EvidenceValues: map[string]float64{"personnelInZone": 1}})
		}
	}
	for i := 0; i < len(s.Cues); i++ {
		for j := i + 1; j < len(s.Cues); j++ {
			aCue, bCue := s.Cues[i], s.Cues[j]
			if !overlap(aCue, bCue) {
				continue
			}
			ae, aok := elements[aCue.ElementID]
			be, bok := elements[bCue.ElementID]
			if !aok || !bok || ae.ID == be.ID {
				continue
			}
			distance := math.Hypot(ae.XMM-be.XMM, ae.YMM-be.YMM)
			if distance < 750 && verticalPathsIntersect(ae.ZMM, aCue.TargetZMM, be.ZMM, bCue.TargetZMM) {
				out = append(out, domain.SafetyFinding{RuleCode: "PATH-001", Severity: "blocking", ElementIDs: []string{ae.ID, be.ID}, CueIDs: []string{aCue.ID, bCue.ID}, Explanation: fmt.Sprintf("并行动作路径水平距离 %.0fmm，小于 750mm 且垂直范围相交", distance), EvidenceValues: map[string]float64{"horizontalDistanceMM": distance, "minimumDistanceMM": 750}})
			}
		}
	}
	return out
}

func (a *Analyzer) timingFindings(s domain.Snapshot) []domain.SafetyFinding {
	var out []domain.SafetyFinding
	for i := 0; i < len(s.Cues); i++ {
		for j := i + 1; j < len(s.Cues); j++ {
			x, y := s.Cues[i], s.Cues[j]
			if overlap(x, y) && (x.OperatorGroup == y.OperatorGroup || x.ElementID == y.ElementID) {
				out = append(out, domain.SafetyFinding{RuleCode: "TIME-001", Severity: "blocking", ElementIDs: unique([]string{x.ElementID, y.ElementID}), CueIDs: []string{x.ID, y.ID}, Explanation: fmt.Sprintf("提示 %d 与 %d 的时间窗重叠，且共享设备或操作组", x.SequenceNo, y.SequenceNo), EvidenceValues: map[string]float64{"firstEndMS": float64(x.StartsAtMS + x.DurationMS), "secondStartMS": float64(y.StartsAtMS)}})
			}
		}
	}
	return out
}

func dependencyFindings(s domain.Snapshot) []domain.SafetyFinding {
	known := map[string]bool{}
	graph := map[string][]string{}
	for _, c := range s.Cues {
		known[c.ID] = true
		graph[c.ID] = c.PreconditionCueIDs
	}
	var out []domain.SafetyFinding
	for _, c := range s.Cues {
		for _, dep := range c.PreconditionCueIDs {
			if !known[dep] {
				out = append(out, domain.SafetyFinding{RuleCode: "LOCK-001", Severity: "blocking", CueIDs: []string{c.ID, dep}, Explanation: fmt.Sprintf("提示 %d 引用了不可达的前置提示 %s", c.SequenceNo, dep), EvidenceValues: map[string]float64{"missing": 1}})
			}
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string, []string)
	visit = func(id string, path []string) {
		if visiting[id] {
			out = append(out, domain.SafetyFinding{RuleCode: "LOCK-002", Severity: "blocking", CueIDs: append(path, id), Explanation: "动作提示互锁关系存在循环", EvidenceValues: map[string]float64{"cycleLength": float64(len(path) + 1)}})
			return
		}
		if visited[id] {
			return
		}
		visiting[id] = true
		for _, next := range graph[id] {
			visit(next, append(path, id))
		}
		visiting[id] = false
		visited[id] = true
	}
	for id := range graph {
		visit(id, nil)
	}
	return out
}

func overlap(a, b domain.MotionCue) bool {
	return a.StartsAtMS < b.StartsAtMS+b.DurationMS && b.StartsAtMS < a.StartsAtMS+a.DurationMS
}
func verticalPathsIntersect(a1, a2, b1, b2 float64) bool {
	amin, amax := math.Min(a1, a2), math.Max(a1, a2)
	bmin, bmax := math.Min(b1, b2), math.Max(b1, b2)
	return amin <= bmax && bmin <= amax
}
func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range in {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
