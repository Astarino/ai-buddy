package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
)

type Sessions interface {
	CreateSession(context.Context, string) (string, error)
	Session(context.Context, string) (string, error)
	DeleteSession(context.Context, string) error
	Metrics(context.Context) (map[string]int, error)
}

type API struct {
	Service      *buddy.Service
	Sessions     Sessions
	DemoToken    string
	StaticDir    string
	SecureCookie bool
	Production   bool
	Login        LoginProvider
}

func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) { respond(w, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /api/bootstrap", func(w http.ResponseWriter, r *http.Request) {
		profiles := a.Service.Catalog.Profiles
		loginURL := ""
		if a.Production {
			profiles = []buddy.Profile{}
			loginURL = "/api/login"
		}
		boundary := a.Service.Model.Boundary()
		endpoint := ""
		if boundary.External {
			endpoint = boundary.Endpoint
		}
		respond(w, map[string]any{"profiles": profiles, "model": a.Service.Model.Name(), "demo": !a.Production, "login_url": loginURL, "external_model": boundary.External, "model_endpoint": endpoint, "consent_to": boundary.Token})
	})
	if a.Production && a.Login != nil {
		mux.HandleFunc("GET /api/login", a.beginLogin)
		mux.HandleFunc("GET /api/login/callback", a.finishLogin)
	}
	mux.HandleFunc("POST /api/session", a.login)
	mux.HandleFunc("DELETE /api/session", a.logout)
	mux.HandleFunc("GET /api/state", a.withProfile(a.state))
	mux.HandleFunc("POST /api/refresh", a.withProfile(a.state))
	a.requestRoutes(mux)
	mux.HandleFunc("GET /api/documents/{id}", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		doc, err := a.Service.Document(r.Context(), p, r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, doc)
	}))
	mux.HandleFunc("POST /api/messages", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			ID              string `json:"id"`
			Text            string `json:"text"`
			ExternalConsent string `json:"external_consent"`
		}
		if !decode(w, r, &input) {
			return
		}
		msg, err := a.Service.ChatWithConsent(r.Context(), p, input.ID, input.Text, input.ExternalConsent)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, msg)
	}))
	mux.HandleFunc("POST /api/actions", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			Kind   string `json:"kind"`
			Reason string `json:"reason"`
		}
		if !decode(w, r, &input) {
			return
		}
		action, err := a.Service.Prepare(r.Context(), p, input.Kind, input.Reason)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, action)
	}))
	mux.HandleFunc("POST /api/actions/{id}/confirm", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var empty struct{}
		if !decode(w, r, &empty) {
			return
		}
		op, err := a.Service.Confirm(r.Context(), p, r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, op)
	}))
	mux.HandleFunc("POST /api/tasks/{id}/complete", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var empty struct{}
		if !decode(w, r, &empty) {
			return
		}
		if err := a.Service.Complete(r.Context(), p, r.PathValue("id")); err != nil {
			failure(w, err)
			return
		}
		a.state(w, r, p)
	}))
	mux.HandleFunc("POST /api/draft", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			Question string `json:"question"`
		}
		if !decode(w, r, &input) {
			return
		}
		draft, err := a.Service.Draft(r.Context(), p, input.Question)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, map[string]string{"draft": draft})
	}))
	if !a.Production {
		mux.HandleFunc("POST /api/demo/events", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
			if !a.operator(r) {
				failure(w, buddy.ErrForbidden)
				return
			}
			var input struct {
				Key    string `json:"key"`
				Status string `json:"status"`
			}
			if !decode(w, r, &input) {
				return
			}
			if a.Service.Operator == nil {
				failure(w, buddy.ErrForbidden)
				return
			}
			if err := a.Service.Operator.Event(r.Context(), p.ID, input.Key, input.Status); err != nil {
				failure(w, err)
				return
			}
			a.state(w, r, p)
		}))
		mux.HandleFunc("GET /api/demo/metrics", func(w http.ResponseWriter, r *http.Request) {
			if !a.operator(r) {
				failure(w, buddy.ErrForbidden)
				return
			}
			counts, err := a.Sessions.Metrics(r.Context())
			if err != nil {
				failure(w, err)
				return
			}
			respond(w, counts)
		})
	}
	mux.HandleFunc("GET /api/diagnostics", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		state, err := a.Service.Repo.Read(r.Context(), p.ID)
		if err != nil {
			failure(w, err)
			return
		}
		events := state.Events
		if audit, ok := a.Service.Repo.(buddy.AuditStore); ok {
			events, err = audit.AuditEvents(r.Context(), p.ID, 200)
			if err != nil {
				failure(w, err)
				return
			}
		}
		respond(w, map[string]any{"model": a.Service.Model.Name(), "prompt_version": model.PromptVersion, "events": events, "simulation": !a.Production, "external_fallback": false})
	}))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { failure(w, buddy.ErrNotFound) })
	mux.HandleFunc("/", a.static)
	return a.audited(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; connect-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; font-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host || (u.Scheme != "http" && u.Scheme != "https") {
					failure(w, buddy.ErrForbidden)
					return
				}
			}
			if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
				failure(w, buddy.ErrInvalid)
				return
			}
		}
		mux.ServeHTTP(w, r)
	}))
}

func (a *API) operator(r *http.Request) bool {
	return a.DemoToken != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Demo-Token")), []byte(a.DemoToken)) == 1
}

func (a *API) withProfile(next func(http.ResponseWriter, *http.Request, buddy.Profile)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(a.sessionCookieName())
		if err != nil {
			unauthorized(w)
			return
		}
		owner, err := a.Sessions.Session(r.Context(), cookie.Value)
		if err != nil {
			unauthorized(w)
			return
		}
		p, err := a.Service.Catalog.Profile(owner)
		if err != nil {
			unauthorized(w)
			return
		}
		if a.Production {
			bound, ok := a.Sessions.(boundSessions)
			if !ok {
				unauthorized(w)
				return
			}
			binding, err := bound.SessionBinding(r.Context(), cookie.Value)
			if err != nil || binding != buddy.ProfileBinding(p) {
				unauthorized(w)
				return
			}
			buddy.SetAuditActor(r.Context(), p.ID)
			if a.Service.Authorizer == nil {
				failure(w, buddy.ErrForbidden)
				return
			}
			if err := a.Service.Authorizer.Check(r.Context(), p, buddy.Resource{System: "ai-buddy", Kind: "account", ID: p.ID}, "read"); err != nil {
				failure(w, err)
				return
			}
		}
		buddy.SetAuditActor(r.Context(), p.ID)
		if expected := r.Header.Get("X-Buddy-Context"); expected != "" && expected != p.ID {
			failure(w, buddy.ErrConflict)
			return
		}
		next(w, r, p)
	}
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	if a.Production {
		failure(w, buddy.ErrForbidden)
		return
	}
	var input struct {
		ProfileID string `json:"profile_id"`
	}
	if !decode(w, r, &input) {
		return
	}
	if _, err := a.Service.Catalog.Profile(input.ProfileID); err != nil {
		failure(w, buddy.ErrInvalid)
		return
	}
	token, err := a.Sessions.CreateSession(r.Context(), input.ProfileID)
	if err != nil {
		failure(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "buddy_session", Value: token, Path: "/", HttpOnly: true, Secure: a.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: 7 * 24 * 3600})
	respond(w, map[string]bool{"ok": true})
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(a.sessionCookieName()); err == nil {
		if err = a.Sessions.DeleteSession(r.Context(), cookie.Value); err != nil {
			failure(w, err)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: a.sessionCookieName(), Value: "", Path: "/", HttpOnly: true, Secure: a.SecureCookie, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	respond(w, map[string]bool{"ok": true})
}

func (a *API) state(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
	view, err := a.Service.View(r.Context(), p)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, view)
}

func (a *API) static(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if a.StaticDir == "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	path := filepath.Join(a.StaticDir, filepath.Clean("/"+r.URL.Path))
	if info, err := os.Stat(path); err == nil && !info.IsDir() {
		http.ServeFile(w, r, path)
		return
	}
	if filepath.Ext(r.URL.Path) != "" {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, filepath.Join(a.StaticDir, "index.html"))
}

func decode(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil || decoder.Decode(new(any)) != io.EOF {
		failure(w, buddy.ErrInvalid)
		return false
	}
	return true
}

func respond(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(value)
}
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	respond(w, map[string]string{"code": "unauthorized", "message": "Выбери тестовый профиль, чтобы продолжить."})
}
func failure(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	status, code, message := http.StatusInternalServerError, "internal", "Не удалось сохранить результат. Повтори попытку."
	switch {
	case errors.Is(err, buddy.ErrNotFound):
		status, code, message = http.StatusNotFound, "not_found", "Материал или операция недоступны. Обнови состояние."
	case errors.Is(err, buddy.ErrForbidden):
		status, code, message = http.StatusForbidden, "forbidden", "Недостаточно прав или не выполнен предыдущий шаг."
	case errors.Is(err, buddy.ErrConflict):
		status, code, message = http.StatusConflict, "conflict", "Состояние уже изменилось. Обнови данные перед следующим действием."
	case errors.Is(err, buddy.ErrInvalid):
		status, code, message = http.StatusBadRequest, "invalid", "Проверь введённые данные."
	case errors.Is(err, buddy.ErrExpired):
		status, code, message = http.StatusGone, "expired", "Карточка устарела. Подготовь действие заново."
	case errors.Is(err, buddy.ErrBusy):
		status, code, message = http.StatusTooManyRequests, "busy", "Модель уже отвечает на другой вопрос. Повтори чуть позже."
	case errors.Is(err, buddy.ErrConsentRequired):
		status, code, message = http.StatusPreconditionRequired, "external_consent", "Подтверди отправку этого вопроса внешней модели."
	case errors.Is(err, buddy.ErrDataPolicy):
		status, code, message = http.StatusForbidden, "data_policy", "Для этого вопроса нужна модель во внутреннем контуре. Командные материалы наружу не отправлены."
	case errors.Is(err, buddy.ErrModelUnavailable):
		status, code, message = http.StatusServiceUnavailable, "model_unavailable", "Модель сейчас недоступна. Вопрос сохранён — его можно повторить."
	case errors.Is(err, buddy.ErrUnavailable), errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		status, code, message = http.StatusServiceUnavailable, "unavailable", "Модель или сервис не ответили. Если отправлял заявку, сначала обнови её состояние: результат мог сохраниться."
	}
	w.WriteHeader(status)
	respond(w, map[string]string{"code": code, "message": message})
}

func HTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 130 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 << 10}
}
