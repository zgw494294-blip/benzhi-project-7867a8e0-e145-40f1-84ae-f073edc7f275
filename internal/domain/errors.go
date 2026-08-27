package domain

import "fmt"

type Error struct {
	Code            string `json:"code"`
	Message         string `json:"message"`
	Field           string `json:"field,omitempty"`
	CurrentRevision int64  `json:"currentRevision,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func Invalid(field, message string) error {
	return &Error{Code: "validation_error", Message: message, Field: field}
}

func Conflict(message string, current int64) error {
	return &Error{Code: "revision_conflict", Message: message, CurrentRevision: current}
}

func StateConflict(state ProductionState, action string) error {
	return &Error{Code: "state_conflict", Message: fmt.Sprintf("状态 %s 不允许执行 %s", state, action)}
}

func NotFound(kind, id string) error {
	return &Error{Code: "not_found", Message: fmt.Sprintf("%s %s 不存在", kind, id)}
}
