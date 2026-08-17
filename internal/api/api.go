package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"microgrid-ops/internal/cabin"
	"microgrid-ops/internal/dispatch"
	"microgrid-ops/internal/parts"
	"microgrid-ops/internal/scheduler"
	"microgrid-ops/internal/workorder"
)

// Server wires the HTTP routes to the orchestrator and scheduler.
type Server struct {
	orch  *dispatch.Orchestrator
	sched *scheduler.Scheduler
	mux   *http.ServeMux
}

// NewServer creates a new HTTP server.
func NewServer(orch *dispatch.Orchestrator, sched *scheduler.Scheduler) *Server {
	s := &Server{orch: orch, sched: sched, mux: http.NewServeMux()}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("POST /api/cabins", s.handleRegisterCabin)
	s.mux.HandleFunc("GET /api/cabins", s.handleListCabins)
	s.mux.HandleFunc("GET /api/cabins/{id}", s.handleGetCabin)
	s.mux.HandleFunc("PUT /api/cabins/{id}/conditions", s.handleSetConditions)
	s.mux.HandleFunc("POST /api/workorders/inspection", s.handleCreateInspection)
	s.mux.HandleFunc("POST /api/workorders/maintenance", s.handleCreateMaintenance)
	s.mux.HandleFunc("GET /api/workorders", s.handleListOrders)
	s.mux.HandleFunc("GET /api/workorders/{id}", s.handleGetOrder)
	s.mux.HandleFunc("POST /api/workorders/{id}/detect-anomaly", s.handleDetectAnomaly)
	s.mux.HandleFunc("POST /api/workorders/{id}/report-anomaly", s.handleReportAnomaly)
	s.mux.HandleFunc("POST /api/workorders/{id}/authorize", s.handleAuthorize)
	s.mux.HandleFunc("POST /api/workorders/{id}/enter", s.handleEnterCabin)
	s.mux.HandleFunc("POST /api/workorders/{id}/request-parts", s.handleRequestParts)
	s.mux.HandleFunc("POST /api/workorders/{id}/confirm-parts", s.handleConfirmParts)
	s.mux.HandleFunc("POST /api/workorders/{id}/emergency-procurement", s.handleEmergencyProcurement)
	s.mux.HandleFunc("POST /api/workorders/{id}/complete", s.handleCompleteRepair)
	s.mux.HandleFunc("POST /api/workorders/{id}/request-restoration", s.handleRequestRestoration)
	s.mux.HandleFunc("POST /api/workorders/{id}/approve-restoration", s.handleApproveRestoration)
	s.mux.HandleFunc("POST /api/workorders/{id}/interrupt-approval", s.handleInterruptApproval)
	s.mux.HandleFunc("POST /api/workorders/{id}/recheck-complete", s.handleRecheckComplete)
	s.mux.HandleFunc("POST /api/workorders/{id}/fail-parts", s.handleFailParts)
	s.mux.HandleFunc("POST /api/workorders/{id}/escalate", s.handleEscalate)
	s.mux.HandleFunc("POST /api/workorders/{id}/resume", s.handleResume)
	s.mux.HandleFunc("POST /api/workorders/{id}/close", s.handleCloseOrder)
	s.mux.HandleFunc("POST /api/workorders/{id}/cancel", s.handleCancelOrder)
	s.mux.HandleFunc("POST /api/parts", s.handleRegisterPart)
	s.mux.HandleFunc("GET /api/parts", s.handleListParts)
	s.mux.HandleFunc("GET /api/notifications", s.handleListNotifications)
}

// Handler returns the http.Handler for use with http.Server.
func (s *Server) Handler() http.Handler { return s.mux }

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func decodeJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

func parsePriority(s string) (workorder.Priority, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	switch s {
	case "", "normal":
		return workorder.PriorityNormal, nil
	case "low":
		return workorder.PriorityLow, nil
	case "high":
		return workorder.PriorityHigh, nil
	case "urgent":
		return workorder.PriorityUrgent, nil
	default:
		if n, err := strconv.Atoi(s); err == nil {
			return workorder.Priority(n), nil
		}
		return 0, fmt.Errorf("invalid priority: %s", s)
	}
}

// ---------------------------------------------------------------------------
// Cabin handlers
// ---------------------------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "time": time.Now().Format(time.RFC3339)})
}

func (s *Server) handleRegisterCabin(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID               string  `json:"id"`
		Name             string  `json:"name"`
		Location         string  `json:"location"`
		TemperatureLimit float64 `json:"temperature_limit"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	c := &cabin.Cabin{
		ID:               body.ID,
		Name:             body.Name,
		Location:         body.Location,
		TemperatureLimit: body.TemperatureLimit,
	}
	if err := s.orch.Cabins().Register(c); err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	result, _ := s.orch.Cabins().Get(body.ID)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListCabins(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Cabins().List())
}

func (s *Server) handleGetCabin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	c, err := s.orch.Cabins().Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}

func (s *Server) handleSetConditions(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Temperature       float64 `json:"temperature"`
		InsulationAlarm   bool    `json:"insulation_alarm"`
		CoolingFanRunning bool    `json:"cooling_fan_running"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.orch.Cabins().SetConditions(id, body.Temperature, body.InsulationAlarm, body.CoolingFanRunning); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	result, _ := s.orch.Cabins().Get(id)
	writeJSON(w, http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// Work order handlers
// ---------------------------------------------------------------------------

func (s *Server) handleCreateInspection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CabinID     string `json:"cabin_id"`
		InspectorID string `json:"inspector_id"`
		Priority    string `json:"priority"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pri, err := parsePriority(body.Priority)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.CreateInspectionOrder(body.CabinID, body.InspectorID, pri)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, wo)
}

func (s *Server) handleCreateMaintenance(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CabinID     string   `json:"cabin_id"`
		EngineerIDs []string `json:"engineer_ids"`
		Priority    string   `json:"priority"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	pri, err := parsePriority(body.Priority)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.CreateMaintenanceOrder(body.CabinID, body.EngineerIDs, pri)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, wo)
}

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Orders().List())
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.Orders().Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleDetectAnomaly(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Temperature       float64 `json:"temperature"`
		InsulationAlarm   bool    `json:"insulation_alarm"`
		CoolingFanRunning bool    `json:"cooling_fan_running"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.DetectAnomaly(id, body.Temperature, body.InsulationAlarm, body.CoolingFanRunning)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleReportAnomaly(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.ReportAnomaly(id, body.Description)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleAuthorize(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ChiefID string `json:"chief_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.AuthorizeMaintenance(id, body.ChiefID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleEnterCabin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.EnterCabin(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleRequestParts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.RequestParts(id, body.SKU, body.Qty)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleConfirmParts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ManagerID string `json:"manager_id"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.ConfirmParts(id, body.ManagerID)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleEmergencyProcurement(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		SKU string `json:"sku"`
		Qty int    `json:"qty"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.EmergencyProcurement(id, body.SKU, body.Qty)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleCompleteRepair(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.CompleteRepair(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleRequestRestoration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.RequestRestoration(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleApproveRestoration(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body struct {
		ChiefID      string `json:"chief_id"`
		BlackStartOK bool   `json:"black_start_ok"`
		OffGridOK    bool   `json:"off_grid_ok"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	wo, err := s.orch.ApproveRestoration(id, body.ChiefID, body.BlackStartOK, body.OffGridOK)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleInterruptApproval(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.InterruptApproval(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleRecheckComplete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.RecheckComplete(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleFailParts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.FailPartsRequisition(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleEscalate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.EscalateOrder(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleResume(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.ResumeFromEscalation(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleCloseOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.CloseOrder(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

func (s *Server) handleCancelOrder(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wo, err := s.orch.CancelOrder(id)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wo)
}

// ---------------------------------------------------------------------------
// Parts handlers
// ---------------------------------------------------------------------------

func (s *Server) handleRegisterPart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SKU      string `json:"sku"`
		Name     string `json:"name"`
		Stock    int    `json:"stock"`
		MinStock int    `json:"min_stock"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &parts.Part{
		SKU:      body.SKU,
		Name:     body.Name,
		Stock:    body.Stock,
		MinStock: body.MinStock,
	}
	if err := s.orch.Parts().RegisterPart(p); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, _ := s.orch.Parts().GetPart(body.SKU)
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handleListParts(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Parts().ListParts())
}

func (s *Server) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.orch.Notifications())
}
