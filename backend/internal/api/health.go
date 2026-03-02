package api

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/shanq/tardi/internal/db"
)

func HealthzHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Tardi-Version", "1")
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func ReadyzHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := db.Ping(r.Context(), pool); err != nil {
			WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
