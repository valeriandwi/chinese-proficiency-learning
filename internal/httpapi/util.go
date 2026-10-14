package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

func contextWithTimeout(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(r.Context(), d)
}

func storeMigrationStatus(ctx context.Context, dbURL string) (applied, pending int, err error) {
	return store.MigrationStatus(ctx, dbURL)
}
