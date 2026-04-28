package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"bragdev-go/internal/httpresp"
	"bragdev-go/internal/logger"
	"bragdev-go/internal/middleware"
	"bragdev-go/internal/usecase"
	"bragdev-go/internal/validation"
)

// RegisterReportRoutes registers report endpoints.
// The router r must already have the Auth middleware applied.
func RegisterReportRoutes(r chi.Router, reportSvc *usecase.ReportService) {
	h := &reportHandler{svc: reportSvc}
	r.Post("/api/reports/ai-summary/custom", h.handleCustomSummary)
}

type reportHandler struct {
	svc *usecase.ReportService
}

type customSummaryRequest struct {
	ReportType   string   `json:"reportType"`
	Category     string   `json:"category"`
	StartDate    string   `json:"startDate"`
	EndDate      string   `json:"endDate"`
	UserPrompt   string   `json:"userPrompt"`
	Repositories []string `json:"repositories"`
}

func (h *reportHandler) handleCustomSummary(w http.ResponseWriter, req *http.Request) {
	userLogin, ok := middleware.UserLoginFromContext(req.Context())
	if !ok {
		httpresp.JSONError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	req.Body = http.MaxBytesReader(w, req.Body, maxBodyBytes)
	var body customSummaryRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		httpresp.JSONError(w, http.StatusBadRequest, "invalid body")
		return
	}

	sdt, edt, err := validation.ValidateDateRange(body.StartDate, body.EndDate)
	if err != nil {
		logger.Debugw("invalid date range", "err", err, "start", body.StartDate, "end", body.EndDate)
		httpresp.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := validation.ValidateRepositories(body.Repositories); err != nil {
		logger.Debugw("invalid repositories", "err", err)
		httpresp.JSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	out, err := h.svc.Generate(req.Context(), usecase.GenerateReportInput{
		UserLogin:    userLogin,
		ReportType:   body.ReportType,
		Category:     body.Category,
		StartDate:    sdt,
		EndDate:      edt,
		UserPrompt:   body.UserPrompt,
		Repositories: body.Repositories,
	})
	if err != nil {
		logger.Errorw("report generation error", "err", err)
		if strings.Contains(err.Error(), "user not found") {
			httpresp.JSONError(w, http.StatusNotFound, "user not found")
			return
		}
		httpresp.JSONError(w, http.StatusInternalServerError, "ai generation failed")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"aiGeneratedReport": out,
		"generatedAt":       time.Now().UTC().Format(time.RFC3339),
		"reportType":        body.ReportType,
	})
}
