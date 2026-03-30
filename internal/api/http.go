package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/srmdn/maild/internal/domain"
	"github.com/srmdn/maild/internal/service"
)

type Handler struct {
	messages *service.MessageService
}

func NewHandler(messages *service.MessageService) *Handler {
	return &Handler{messages: messages}
}

func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("/v1/messages", h.createMessage)
}

type createMessageRequest struct {
	WorkspaceID int64  `json:"workspace_id"`
	FromEmail   string `json:"from_email"`
	ToEmail     string `json:"to_email"`
	Subject     string `json:"subject"`
	BodyText    string `json:"body_text"`
}

func (h *Handler) createMessage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req createMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.WorkspaceID == 0 {
		req.WorkspaceID = 1
	}
	if strings.TrimSpace(req.FromEmail) == "" || strings.TrimSpace(req.ToEmail) == "" || strings.TrimSpace(req.Subject) == "" {
		http.Error(w, "from_email, to_email, and subject are required", http.StatusBadRequest)
		return
	}

	m, err := h.messages.QueueMessage(r.Context(), req.WorkspaceID, req.FromEmail, req.ToEmail, req.Subject, req.BodyText)
	if err != nil {
		http.Error(w, "failed to queue message", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusCreated, m)
}

func writeJSON(w http.ResponseWriter, status int, payload domain.Message) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
