package httpui

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"stageclearance/internal/analyzer"
	"stageclearance/internal/store"
	"stageclearance/internal/workflow"
)

func TestWorkbenchAndValidationError(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "http.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	handler := New(workflow.New(db, analyzer.New())).Handler()
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/", nil))
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "<body>") {
		t.Fatalf("workbench response %d", recorder.Code)
	}
	recorder = httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/productions", strings.NewReader(`{"unknown":true}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_json") {
		t.Fatalf("validation response %d %s", recorder.Code, recorder.Body.String())
	}
}
