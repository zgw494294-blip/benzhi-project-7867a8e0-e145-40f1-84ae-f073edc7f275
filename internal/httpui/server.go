package httpui

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"stageclearance/internal/domain"
	"stageclearance/internal/workflow"
)

type Server struct {
	service *workflow.Service
	mux     *http.ServeMux
	assets  fs.FS
}

func New(service *workflow.Service) *Server {
	assets, _ := fs.Sub(embeddedAssets, "assets")
	s := &Server{service: service, mux: http.NewServeMux(), assets: assets}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return securityHeaders(s.mux) }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.WorkbenchHandler)
	s.mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(s.assets))))
	s.mux.HandleFunc("GET /api/productions", s.ListProductionsHandler)
	s.mux.HandleFunc("POST /api/productions", s.CreateProductionHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/clone", s.CloneProductionHandler)
	s.mux.HandleFunc("GET /api/productions/{id}", s.GetProductionHandler)
	s.mux.HandleFunc("PATCH /api/productions/{id}", s.UpdateProductionHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/elements", s.AddElementHandler)
	s.mux.HandleFunc("PUT /api/productions/{id}/elements/{elementID}", s.UpdateElementHandler)
	s.mux.HandleFunc("DELETE /api/productions/{id}/elements/{elementID}", s.RemoveElementHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/elements/batch", s.BatchElementsHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/cues", s.AddCueHandler)
	s.mux.HandleFunc("PUT /api/productions/{id}/cues/{cueID}", s.UpdateCueHandler)
	s.mux.HandleFunc("DELETE /api/productions/{id}/cues/{cueID}", s.RemoveCueHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/cues/schedule", s.ScheduleCuesHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/analysis", s.AnalyzeHandler)
	s.mux.HandleFunc("GET /api/productions/{id}/analysis/history", s.AnalysisHistoryHandler)
	s.mux.HandleFunc("GET /api/productions/{id}/analysis/compare", s.CompareAnalysisHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/findings/{findingID}/remediation", s.RemediateFindingHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/evidence", s.AddEvidenceHandler)
	s.mux.HandleFunc("GET /api/productions/{id}/evidence/coverage", s.EvidenceCoverageHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/rehearsal/submit", s.SubmitRehearsalHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/reviews", s.ReviewHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/review-items/{itemID}/response", s.ReviewResponseHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/release", s.ReleaseHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/release/{credentialID}/revoke", s.RevokeCredentialHandler)
	s.mux.HandleFunc("POST /api/productions/{id}/release/{credentialID}/reissue", s.ReissueCredentialHandler)
	s.mux.HandleFunc("GET /api/productions/{id}/audit", s.AuditHandler)
	s.mux.HandleFunc("POST /api/credentials/verify", s.VerifyCredentialHandler)
}

func (s *Server) WorkbenchHandler(w http.ResponseWriter, r *http.Request) {
	content, err := fs.ReadFile(s.assets, "index.html")
	if err != nil {
		writeError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(content)
}

func (s *Server) ListProductionsHandler(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.List(r.Context())
	writeResult(w, items, err, http.StatusOK)
}
func (s *Server) GetProductionHandler(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.Get(r.Context(), r.PathValue("id"))
	writeResult(w, item, err, http.StatusOK)
}

func (s *Server) CreateProductionHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.CreateProductionCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.CreateProduction(r.Context(), command)
	writeResult(w, item, err, http.StatusCreated)
}
func (s *Server) CloneProductionHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.CloneProductionCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.CloneProduction(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusCreated)
}
func (s *Server) UpdateProductionHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.UpdateProductionCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.UpdateProduction(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AddElementHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AddElementCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.AddElement(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusCreated)
}
func (s *Server) UpdateElementHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AddElementCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.UpdateElement(r.Context(), r.PathValue("id"), r.PathValue("elementID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) RemoveElementHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.CommandMeta
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.RemoveElement(r.Context(), r.PathValue("id"), r.PathValue("elementID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) BatchElementsHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.BatchElementsCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.BatchElements(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AddCueHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AddCueCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.AddCue(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusCreated)
}
func (s *Server) UpdateCueHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AddCueCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.UpdateCue(r.Context(), r.PathValue("id"), r.PathValue("cueID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) RemoveCueHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.CommandMeta
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.RemoveCue(r.Context(), r.PathValue("id"), r.PathValue("cueID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) ScheduleCuesHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.ScheduleCuesCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.ScheduleCues(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AnalyzeHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AnalyzeCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.Analyze(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AnalysisHistoryHandler(w http.ResponseWriter, r *http.Request) {
	items, err := s.service.AnalysisHistory(r.Context(), r.PathValue("id"))
	writeResult(w, items, err, http.StatusOK)
}
func (s *Server) CompareAnalysisHandler(w http.ResponseWriter, r *http.Request) {
	older, err := strconv.ParseInt(r.URL.Query().Get("olderRevision"), 10, 64)
	if err != nil {
		writeError(w, domain.Invalid("olderRevision", "较旧分析修订必须为整数"))
		return
	}
	newer, err := strconv.ParseInt(r.URL.Query().Get("newerRevision"), 10, 64)
	if err != nil {
		writeError(w, domain.Invalid("newerRevision", "较新分析修订必须为整数"))
		return
	}
	item, compareErr := s.service.CompareAnalysis(r.Context(), r.PathValue("id"), older, newer)
	writeResult(w, item, compareErr, http.StatusOK)
}
func (s *Server) RemediateFindingHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.RemediateCommand
	if !decode(w, r, &command) {
		return
	}
	command.FindingID = r.PathValue("findingID")
	item, err := s.service.Remediate(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AddEvidenceHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.AddEvidenceCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.AddEvidence(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusCreated)
}
func (s *Server) EvidenceCoverageHandler(w http.ResponseWriter, r *http.Request) {
	item, err := s.service.EvidenceCoverage(r.Context(), r.PathValue("id"))
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) SubmitRehearsalHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.CommandMeta
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.SubmitRehearsal(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) ReviewHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.ReviewCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.Review(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) ReviewResponseHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.ReviewResponseCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.RespondReviewItem(r.Context(), r.PathValue("id"), r.PathValue("itemID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) ReleaseHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.ReleaseCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.Release(r.Context(), r.PathValue("id"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) RevokeCredentialHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.RevokeCredentialCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.RevokeCredential(r.Context(), r.PathValue("id"), r.PathValue("credentialID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) ReissueCredentialHandler(w http.ResponseWriter, r *http.Request) {
	var command workflow.ReissueCredentialCommand
	if !decode(w, r, &command) {
		return
	}
	item, err := s.service.ReissueCredential(r.Context(), r.PathValue("id"), r.PathValue("credentialID"), command)
	writeResult(w, item, err, http.StatusOK)
}
func (s *Server) AuditHandler(w http.ResponseWriter, r *http.Request) {
	events, err := s.service.Audit(r.Context(), r.PathValue("id"))
	writeResult(w, events, err, http.StatusOK)
}

func (s *Server) VerifyCredentialHandler(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if strings.TrimSpace(input.Code) == "" {
		writeError(w, domain.Invalid("code", "校验码不能为空"))
		return
	}
	result, err := s.service.Verify(r.Context(), input.Code)
	if err != nil {
		var de *domain.Error
		if errors.As(err, &de) && de.Code == "not_found" {
			writeJSON(w, http.StatusOK, result)
			return
		}
	}
	writeResult(w, result, err, http.StatusOK)
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, &domain.Error{Code: "invalid_json", Message: "请求 JSON 无效或包含未知字段"})
		return false
	}
	if decoder.Decode(&struct{}{}) == nil {
		writeError(w, &domain.Error{Code: "invalid_json", Message: "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}

func writeResult(w http.ResponseWriter, value any, err error, status int) {
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, status, value)
}
func writeError(w http.ResponseWriter, err error) {
	var de *domain.Error
	if errors.As(err, &de) {
		status := http.StatusBadRequest
		if de.Code == "not_found" {
			status = http.StatusNotFound
		}
		if de.Code == "revision_conflict" || de.Code == "state_conflict" || de.Code == "storage_conflict" {
			status = http.StatusConflict
		}
		writeJSON(w, status, map[string]any{"error": de})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"error": map[string]string{"code": "internal_error", "message": "服务处理请求失败"}})
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; connect-src 'self'")
		next.ServeHTTP(w, r)
	})
}
