package domain

import "fmt"

func Transition(production *ProductionFile, target ProductionState, action string) error {
	if production == nil {
		return Invalid("production", "制作档案不能为空")
	}
	if !CanTransition(production.State, target) {
		return StateConflict(production.State, action)
	}
	production.State = target
	return nil
}

func StateLabel(state ProductionState) string {
	labels := map[ProductionState]string{
		StateDraft: "草拟", StatePendingAnalysis: "待分析", StatePendingRehearsal: "待排练",
		StatePendingReview: "待复核", StateRemediation: "整改中", StateFrozen: "已冻结", StateReleased: "已放行",
	}
	if label, exists := labels[state]; exists {
		return label
	}
	return fmt.Sprintf("未知状态(%s)", state)
}
