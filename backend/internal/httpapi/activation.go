package httpapi

import (
	"net/http"
	"strconv"

	"github.com/afnews/backend/internal/store"
)

// handleAdminActivationWaves reports the staged activation strategy (§268, §310):
// the catalog is broad, but production polling is deliberately selective.
func (s *Server) handleAdminActivationWaves(w http.ResponseWriter, r *http.Request) {
	waves, err := s.store.ActivationWaves(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"waves": waves,
		"notes": []string{
			"Wave 1 (CORE) is the validated direct/official set and is the production default.",
			"Waves 2-4 are discovery-heavy and should only run after feed health is stable.",
			"Activation never deletes feeds or resets runtime history.",
		},
	})
}

// handleAdminActivateWave enables exactly the feeds belonging to one rollout wave.
func (s *Server) handleAdminActivateWave(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Wave int `json:"wave"`
	}
	if err := decodeJSON(r, &body); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", err.Error(), nil)
		return
	}
	if body.Wave < 1 || body.Wave > 4 {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_ARGUMENT", "wave must be between 1 and 4", nil)
		return
	}
	if !s.requireAction(w, r, "feed_edit") {
		return
	}
	enabled, disabled, err := s.store.ApplyActivationWave(r.Context(), store.ActivationWave(body.Wave))
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "INTERNAL", err.Error(), nil)
		return
	}
	actor := "unknown"
	if claims := claimsFrom(r.Context()); claims != nil {
		actor = claims.Username
	}
	_ = s.store.Audit(r.Context(), actor, "ACTIVATE_WAVE", "feed", strconv.Itoa(body.Wave), "",
		strconv.Itoa(enabled)+"/"+strconv.Itoa(disabled))
	s.writeJSON(w, http.StatusOK, map[string]any{
		"wave": body.Wave, "enabled": enabled, "disabled": disabled,
		"note": "Only the polling flag changed; health history and article data are untouched.",
	})
}
