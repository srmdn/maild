package api

import (
	"errors"
	"github.com/srmdn/maild/internal/auth"
	"net/http"
)

func resolveWorkspace(w http.ResponseWriter, r *http.Request, requested int64) (int64, bool) {
	id, err := auth.Workspace(r.Context(), requested)
	if err == nil {
		return id, true
	}
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "unauthorized")
	case errors.Is(err, auth.ErrInvalidWorkspace):
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
	default:
		writeError(w, http.StatusForbidden, "forbidden")
	}
	return 0, false
}

func workspaceFromQuery(w http.ResponseWriter, r *http.Request) (int64, bool) {
	values := r.URL.Query()["workspace_id"]
	if len(values) > 1 {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return 0, false
	}
	id, err := parseInt64Query(r, "workspace_id", 0)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace_id")
		return 0, false
	}
	return resolveWorkspace(w, r, id)
}
