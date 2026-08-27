package analyzer

import (
	"stageclearance/internal/domain"
	"testing"
)

func TestAnalyzeDeterministicLoadFinding(t *testing.T) {
	s := domain.Snapshot{Production: domain.ProductionFile{ID: "p", Revision: 3}, Elements: []domain.RiggingElement{{ID: "e", Label: "吊点", RatedLoadKG: 100, AppliedLoadKG: 80, SafetyFactor: 2}}}
	a := New()
	first := a.Analyze(s)
	second := a.Analyze(s)
	if len(first) != 1 || first[0].RuleCode != "LOAD-001" {
		t.Fatalf("unexpected findings: %#v", first)
	}
	if first[0].ID != second[0].ID {
		t.Fatal("finding id is not deterministic")
	}
}
