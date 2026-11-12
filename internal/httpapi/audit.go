package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}

func (w *statusWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (a *API) audited(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID, start := buddy.NewID(), time.Now()
		r = r.WithContext(buddy.WithAudit(r.Context(), "anonymous", requestID))
		w.Header().Set("X-Request-ID", requestID)
		capture := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(capture, r)
		if capture.status < 400 {
			return
		}
		store, ok := a.Service.Repo.(buddy.AuditStore)
		if !ok {
			return
		}
		actor, _ := buddy.AuditIdentity(r.Context())
		// Unknown paths can contain arbitrary input, including credentials; keep only a digest.
		digest := sha256.Sum256([]byte(r.URL.Path))
		decision := "deny"
		if capture.status >= 500 {
			decision = "error"
		}
		err := store.AppendAudit(r.Context(), buddy.Event{ActorID: actor, OwnerID: actor, Type: "http_error", ObjectID: hex.EncodeToString(digest[:]), Decision: decision, Outcome: strconv.Itoa(capture.status), CorrelationID: requestID, At: time.Now().UTC(), DurationMS: time.Since(start).Milliseconds()})
		if err != nil {
			slog.Error("could not persist request audit")
		}
	})
}
