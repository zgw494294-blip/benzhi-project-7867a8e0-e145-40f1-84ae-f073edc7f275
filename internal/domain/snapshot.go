package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

func (s Snapshot) ElementByID(id string) (RiggingElement, bool) {
	for _, element := range s.Elements {
		if element.ID == id {
			return element, true
		}
	}
	return RiggingElement{}, false
}

func (s Snapshot) CueByID(id string) (MotionCue, bool) {
	for _, cue := range s.Cues {
		if cue.ID == id {
			return cue, true
		}
	}
	return MotionCue{}, false
}

func (s Snapshot) FindingByID(id string) (SafetyFinding, bool) {
	for _, finding := range s.Findings {
		if finding.ID == id {
			return finding, true
		}
	}
	return SafetyFinding{}, false
}

func (s Snapshot) OpenBlockingFindings() []SafetyFinding {
	var findings []SafetyFinding
	for _, finding := range s.Findings {
		if finding.Severity == "blocking" && finding.ResolutionState != "closed" {
			findings = append(findings, finding)
		}
	}
	sort.Slice(findings, func(i, j int) bool { return findings[i].ID < findings[j].ID })
	return findings
}

func (s Snapshot) LatestEvidenceForAnalysis() map[string]RehearsalEvidence {
	latest := make(map[string]RehearsalEvidence)
	for _, evidence := range s.Evidence {
		if evidence.AnalysisRevision != s.Production.LastAnalysisRevision {
			continue
		}
		current, exists := latest[evidence.CueID]
		if !exists || evidence.RunNumber > current.RunNumber || evidence.RunNumber == current.RunNumber && evidence.PerformedAt.After(current.PerformedAt) {
			latest[evidence.CueID] = evidence
		}
	}
	return latest
}

func (s Snapshot) RehearsalReady() (bool, []string) {
	latest := s.LatestEvidenceForAnalysis()
	var missing []string
	for _, cue := range s.Cues {
		evidence, exists := latest[cue.ID]
		if !exists || evidence.Result != "passed" {
			missing = append(missing, cue.ID)
		}
	}
	sort.Strings(missing)
	return len(missing) == 0, missing
}

func (s Snapshot) ValidateIntegrity() error {
	if err := ValidateProduction(s.Production); err != nil {
		return err
	}
	elementIDs := make(map[string]bool)
	for _, element := range s.Elements {
		if element.ProductionID != s.Production.ID {
			return Invalid("elements.productionID", "吊挂元素不属于当前制作")
		}
		if elementIDs[element.ID] {
			return Invalid("elements.id", "吊挂元素标识不能重复")
		}
		elementIDs[element.ID] = true
	}
	for _, element := range s.Elements {
		others := make([]RiggingElement, 0, len(s.Elements)-1)
		for _, candidate := range s.Elements {
			if candidate.ID != element.ID {
				others = append(others, candidate)
			}
		}
		if err := ValidateElement(element, s.Production, others); err != nil {
			return err
		}
	}
	cueIDs := make(map[string]bool)
	sequences := make(map[int]bool)
	for _, cue := range s.Cues {
		if cue.ProductionID != s.Production.ID {
			return Invalid("cues.productionID", "动作提示不属于当前制作")
		}
		if cueIDs[cue.ID] || sequences[cue.SequenceNo] {
			return Invalid("cues", "提示标识和序号不能重复")
		}
		cueIDs[cue.ID], sequences[cue.SequenceNo] = true, true
	}
	for _, cue := range s.Cues {
		others := make([]MotionCue, 0, len(s.Cues)-1)
		for _, candidate := range s.Cues {
			if candidate.ID != cue.ID {
				others = append(others, candidate)
			}
		}
		if err := ValidateCue(cue, s.Elements, others); err != nil {
			return err
		}
	}
	return nil
}

func VerifyManifest(snapshot Snapshot, manifest FrozenManifest) (bool, error) {
	material := snapshot
	material.Production.Revision = manifest.Revision
	material.Production.State = StateFrozen
	recomputed, err := BuildManifest(material, manifest.FrozenAt)
	if err != nil {
		return false, err
	}
	canonicalSum := sha256.Sum256([]byte(manifest.Canonical))
	canonicalSHA := hex.EncodeToString(canonicalSum[:])
	return strings.EqualFold(recomputed.SHA256, manifest.SHA256) && strings.EqualFold(canonicalSHA, manifest.SHA256), nil
}
