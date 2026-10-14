package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// DefaultCurriculumVersion is the syllabus a learner is pointed at when they have
// not chosen one. HSK 2.0 levels 1-6 remain the sitting format the app ships
// content for first; hsk3.0 is importable data alongside it.
const DefaultCurriculumVersion = "hsk2.0"

func (s *Server) defaultCurriculumVersion(r *http.Request) string {
	if v := r.URL.Query().Get("version"); v != "" {
		return v
	}
	return DefaultCurriculumVersion
}

func chiURLParam(r *http.Request, name string) string {
	return chi.URLParam(r, name)
}

// parseLimitQuery reads a positive integer query parameter with a default.
func parseLimitQuery(r *http.Request, name string, def int) int {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return def
	}
	n, err := strconvAtoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	return n
}
