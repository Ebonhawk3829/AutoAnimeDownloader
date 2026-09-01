package api

import (
	"AutoAnimeDownloader/src/internal/files"
	"AutoAnimeDownloader/src/internal/logger"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// ptr devolve um ponteiro para s. Auxiliar local para normalizacao do override.
func ptr(s string) *string { return &s }

// Progress e SearchQueryOverride sao PONTEIROS porque o PUT e parcial: ausente tem de ser
// distinguivel de zero/vazio, ou um corpo sem os campos zeraria o que ja esta salvo.
type animeSettingsRequest struct {
	Progress            *int    `json:"progress"`
	SearchQueryOverride *string `json:"search_query_override"`
}

func handleAnimeSettings(server *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.Atoi(idStr)
		if err != nil || id <= 0 {
			JSONError(w, http.StatusBadRequest, "INVALID_ID", "Invalid anime ID")
			return
		}

		switch r.Method {
		case http.MethodGet:
			settings, err := server.FileManager.LoadAnimeSettings(id)
			if err != nil {
				logger.Logger.Error().Err(err).Int("anime_id", id).Msg("Failed to load anime settings")
				JSONInternalError(w, err)
				return
			}
			JSONSuccess(w, http.StatusOK, settings)

		case http.MethodPut:
			var req animeSettingsRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				JSONError(w, http.StatusBadRequest, "INVALID_BODY", "Invalid request body")
				return
			}
			if req.Progress != nil && *req.Progress < 0 {
				JSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", "Progress must be non-negative")
				return
			}
			if req.SearchQueryOverride != nil && strings.TrimSpace(*req.SearchQueryOverride) == "" {
				// Normaliza branco para vazio: limpar o campo no UI tem de remover o override,
				// nao salvar uma string de espacos que quebraria a busca.
				req.SearchQueryOverride = ptr("")
			}
			if req.SearchQueryOverride != nil && strings.TrimSpace(*req.SearchQueryOverride) == "" {
				// Normaliza branco para vazio: limpar o campo no UI tem de remover o override,
				// nao salvar uma string de espacos que quebraria a busca.
				req.SearchQueryOverride = ptr("")
			}

			existing, err := server.FileManager.LoadAnimeSettings(id)
			if err != nil {
				logger.Logger.Error().Err(err).Int("anime_id", id).Msg("Failed to load anime settings")
				JSONInternalError(w, err)
				return
			}

			settings := files.AnimeSettings{}
			if existing != nil {
				settings = *existing
			}
			if req.Progress != nil {
				settings.Progress = *req.Progress
			}
			if req.SearchQueryOverride != nil {
				settings.SearchQueryOverride = strings.TrimSpace(*req.SearchQueryOverride)
			}
			if req.SearchQueryOverride != nil {
				settings.SearchQueryOverride = strings.TrimSpace(*req.SearchQueryOverride)
			}

			if err := server.FileManager.SaveAnimeSettings(id, settings); err != nil {
				logger.Logger.Error().Err(err).Int("anime_id", id).Msg("Failed to save anime settings")
				JSONInternalError(w, err)
				return
			}

			JSONSuccess(w, http.StatusOK, nil)

		default:
			JSONError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Only GET and PUT methods are allowed")
		}
	}
}
