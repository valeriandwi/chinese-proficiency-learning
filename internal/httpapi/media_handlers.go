package httpapi

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/valeriandwipurnomo/hanpath/internal/media"
)

// handleMedia serves a stored asset by key.
//
// Cache-Control is immutable and effectively eternal because the key is a hash of
// the asset's own bytes: if the audio changes, the key changes with it, so a stale
// cache entry is impossible by construction. This is the payoff for content-derived
// keys rather than sequential file names.
func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request) {
	if s.media == nil {
		writeError(w, r, http.StatusServiceUnavailable, "media_unavailable",
			"this deployment has no media store configured", nil)
		return
	}
	key := chi.URLParam(r, "key")
	data, contentType, err := s.media.Get(r.Context(), key)
	switch {
	case errors.Is(err, media.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "no such asset", nil)
		return
	case errors.Is(err, media.ErrBadKey):
		// A key is generated internally; a bad one means a crafted request.
		writeError(w, r, http.StatusNotFound, "not_found", "no such asset", nil)
		return
	case err != nil:
		s.log.ErrorContext(r.Context(), "reading media asset", "key", key, "error", err)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", nil)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", itoa(len(data)))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		s.log.WarnContext(r.Context(), "writing media asset", "key", key, "error", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
