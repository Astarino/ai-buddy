package corporate

import (
	"context"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/storage"
	"github.com/jackc/pgx/v5"
)

type Server struct {
	DB            *storage.Postgres
	Token         string
	OperatorToken string
	Catalog       buddy.Catalog
	ResponseDelay time.Duration
	mu            sync.Mutex
	faults        map[string]string
}

func NewServer(db *storage.Postgres, token, operator string) *Server {
	return &Server{DB: db, Token: token, OperatorToken: operator, Catalog: buddy.LoadCatalog(), ResponseDelay: 5 * time.Second, faults: map[string]string{}}
}

func authorized(r *http.Request, token string) bool {
	return token != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) == 1
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	correlation := r.Header.Get("X-Buddy-Request-ID")
	if raw, err := hex.DecodeString(correlation); err != nil || len(raw) != 16 {
		correlation = buddy.NewID()
	}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/health" && r.Method == http.MethodGet {
		w.Write([]byte(`{"status":"ok","simulation":true}`))
		return
	}
	if r.URL.Path == "/events" {
		if !authorized(r, s.OperatorToken) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var input struct {
			UserID string `json:"user_id"`
			Key    string `json:"key"`
			Status string `json:"status"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&input) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if _, err := s.Catalog.Profile(input.UserID); err != nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if input.Key == "" && (input.Status == "normal" || input.Status == "unavailable" || input.Status == "lost_response") {
			s.mu.Lock()
			s.faults[input.UserID] = input.Status
			s.mu.Unlock()
			w.Write([]byte(`{"ok":true}`))
			return
		}
		ctx := buddy.WithAudit(r.Context(), "demo-operator", correlation)
		err := s.transition(ctx, input.UserID, input.Key, input.Status)
		write(w, map[string]bool{"ok": err == nil}, err)
		return
	}
	if !authorized(r, s.Token) {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if r.URL.Path != "/operations" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	switch r.Method {
	case http.MethodGet:
		owner := r.URL.Query().Get("user")
		if s.fault(owner) == "unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		ops, err := s.list(r.Context(), owner)
		write(w, ops, err)
	case http.MethodPost:
		var input struct {
			UserID string       `json:"user_id"`
			Action buddy.Action `json:"action"`
		}
		decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&input) != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if s.fault(input.UserID) == "unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		op, err := s.create(buddy.WithAudit(r.Context(), "demo-service", correlation), input.UserID, input.Action)
		if err == nil && s.fault(input.UserID) == "lost_response" {
			timer := time.NewTimer(s.ResponseDelay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		write(w, op, err)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) fault(owner string) string { s.mu.Lock(); defer s.mu.Unlock(); return s.faults[owner] }

func write(w http.ResponseWriter, value any, err error) {
	if err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, buddy.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, buddy.ErrForbidden) {
			status = http.StatusForbidden
		}
		if errors.Is(err, buddy.ErrConflict) {
			status = http.StatusConflict
		}
		if errors.Is(err, buddy.ErrInvalid) {
			status = http.StatusBadRequest
		}
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]string{"error": "simulation request failed"})
		return
	}
	json.NewEncoder(w).Encode(value)
}

func (s *Server) list(ctx context.Context, owner string) ([]buddy.Operation, error) {
	rows, err := s.DB.Pool.Query(ctx, "SELECT body FROM corporate_operations WHERE user_id=$1 ORDER BY updated_at", owner)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ops := []buddy.Operation{}
	for rows.Next() {
		var op buddy.Operation
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err = json.Unmarshal(raw, &op); err != nil {
			return nil, err
		}
		ops = append(ops, op)
	}
	return ops, rows.Err()
}

func (s *Server) create(ctx context.Context, owner string, a buddy.Action) (buddy.Operation, error) {
	p, err := s.Catalog.Profile(owner)
	if err != nil {
		return buddy.Operation{}, buddy.ErrForbidden
	}
	if len(a.ID) < 8 || len(a.ID) > 80 || len(a.Reason) < 8 {
		return buddy.Operation{}, buddy.ErrInvalid
	}
	valid := (a.Kind == "access" && a.Resource == p.Space && a.Level == "Чтение") || (a.Kind == "issue" && a.Resource == p.Project+"-101" && a.Level == "Назначить на себя")
	if !valid {
		return buddy.Operation{}, buddy.ErrForbidden
	}
	tx, err := s.DB.Pool.Begin(ctx)
	if err != nil {
		return buddy.Operation{}, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", owner); err != nil {
		return buddy.Operation{}, err
	}
	var existing []byte
	err = tx.QueryRow(ctx, "SELECT body FROM corporate_operations WHERE operation_key=$1", a.ID).Scan(&existing)
	if err == nil {
		var op buddy.Operation
		if err = json.Unmarshal(existing, &op); err != nil {
			return op, err
		}
		if op.UserID != owner || op.Kind != a.Kind || op.Resource != a.Resource || op.Reason != a.Reason || op.Level != a.Level {
			return buddy.Operation{}, buddy.ErrConflict
		}
		return op, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return buddy.Operation{}, err
	}
	rows, err := tx.Query(ctx, "SELECT body FROM corporate_operations WHERE user_id=$1", owner)
	if err != nil {
		return buddy.Operation{}, err
	}
	granted := false
	conflict := false
	for rows.Next() {
		var op buddy.Operation
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return op, err
		}
		if err = json.Unmarshal(raw, &op); err != nil {
			rows.Close()
			return op, err
		}
		if op.Kind == "access" && op.Status == "granted" {
			granted = true
		}
		if op.Kind == a.Kind && op.Resource == a.Resource && op.Status != "rejected" && op.Status != "revoked" {
			conflict = true
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return buddy.Operation{}, err
	}
	if conflict {
		return buddy.Operation{}, buddy.ErrConflict
	}
	if a.Kind == "issue" && !granted {
		return buddy.Operation{}, buddy.ErrForbidden
	}
	op := buddy.Operation{ID: "REQ-" + strings.ToUpper(a.ID[:6]), Key: a.ID, UserID: owner, Kind: a.Kind, Resource: a.Resource, Level: a.Level, Reason: a.Reason, Status: "pending", Note: "Ожидает владельца пространства. Срок не установлен.", UpdatedAt: time.Now().UTC()}
	if a.Kind == "issue" {
		op.ID = a.Resource
		op.Status = "assigned"
		op.Note = "Учебная задача назначена на стажёра. Завершение подтвердит сервис Jira."
	}
	raw, err := json.Marshal(op)
	if err != nil {
		return op, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO corporate_operations(operation_key,user_id,body) VALUES($1,$2,$3)", a.ID, owner, raw); err != nil {
		return op, err
	}
	if err := s.DB.AuditOperation(ctx, tx, owner, a.ID, "operation_created", op.Status); err != nil {
		return buddy.Operation{}, err
	}
	return op, tx.Commit(ctx)
}

func (s *Server) transition(ctx context.Context, owner, key, status string) error {
	tx, err := s.DB.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var raw []byte
	err = tx.QueryRow(ctx, "SELECT body FROM corporate_operations WHERE operation_key=$1 AND user_id=$2 FOR UPDATE", key, owner).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return buddy.ErrNotFound
	}
	if err != nil {
		return err
	}
	var op buddy.Operation
	if err = json.Unmarshal(raw, &op); err != nil {
		return err
	}
	allowed := map[string][]string{"pending": {"approved", "rejected"}, "approved": {"granted", "rejected"}, "granted": {"revoked"}, "assigned": {"done"}}
	valid := status == op.Status
	for _, next := range allowed[op.Status] {
		if next == status {
			valid = true
		}
	}
	if !valid {
		return buddy.ErrConflict
	}
	previous := op.Status
	op.Status, op.UpdatedAt = status, time.Now().UTC()
	op.Note = map[string]string{"approved": "Владелец согласовал. Сервис ещё не выдал права.", "granted": "Тестовое событие: сервис подтвердил выдачу прав.", "rejected": "Тестовый отказ владельца: уточни необходимость ресурса с наставником.", "revoked": "Тестовое событие: права отозваны.", "done": "Тестовое событие Jira: задача и ревью завершены.", "pending": op.Note, "assigned": op.Note}[status]
	raw, err = json.Marshal(op)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, "UPDATE corporate_operations SET body=$2,updated_at=now() WHERE operation_key=$1", key, raw); err != nil {
		return err
	}
	if status != previous {
		if err := s.DB.AuditOperation(ctx, tx, owner, key, "resource_transition", status); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
