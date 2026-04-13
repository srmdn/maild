package api

import (
	"net/http"
)

func (h *Handler) operatorDashboardUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	data := PageData{
		WorkspaceID: workspaceID,
		Active:      "dashboard",
	}
	if err := RenderPage(w, data, "dashboard.html"); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handler) onboardingUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	data := PageData{
		WorkspaceID: workspaceID,
		Active:      "onboarding",
	}
	if err := RenderPage(w, data, "onboarding.html"); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handler) incidentUI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	workspaceID, err := parseInt64Query(r, "workspace_id", 1)
	if err != nil {
		http.Error(w, "invalid workspace_id", http.StatusBadRequest)
		return
	}

	data := PageData{
		WorkspaceID: workspaceID,
		Active:      "incidents",
	}
	if err := RenderPage(w, data, "incidents.html"); err != nil {
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
