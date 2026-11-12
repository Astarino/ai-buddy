package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/identity"
)

type LoginProvider interface {
	Begin(context.Context) (string, string, error)
	Complete(context.Context, string, string, string) (identity.Identity, error)
}

func (a *API) sessionCookieName() string {
	if a.Production {
		return "__Host-buddy_session"
	}
	return "buddy_session"
}

type boundSessions interface {
	ClaimPrincipal(context.Context, string, string, string) error
	BindSession(context.Context, string, string, time.Time) error
	SessionBinding(context.Context, string) (string, error)
}

func (a *API) beginLogin(w http.ResponseWriter, r *http.Request) {
	location, state, err := a.Login.Begin(r.Context())
	if err != nil {
		failure(w, buddy.ErrUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "__Host-buddy_login", Value: state, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 600})
	http.Redirect(w, r, location, http.StatusSeeOther)
}

func (a *API) finishLogin(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("__Host-buddy_login")
	http.SetCookie(w, &http.Cookie{Name: "__Host-buddy_login", Value: "", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if err != nil {
		failure(w, buddy.ErrForbidden)
		return
	}
	principal, err := a.Login.Complete(r.Context(), r.URL.Query().Get("state"), cookie.Value, r.URL.Query().Get("code"))
	if err != nil {
		failure(w, buddy.ErrForbidden)
		return
	}
	profile, err := a.Service.Catalog.CorporateProfile(principal.Issuer, principal.Subject)
	if err != nil {
		failure(w, buddy.ErrForbidden)
		return
	}
	bound, ok := a.Sessions.(boundSessions)
	if !ok {
		failure(w, buddy.ErrUnavailable)
		return
	}
	buddy.SetAuditActor(r.Context(), profile.ID)
	if err := bound.ClaimPrincipal(r.Context(), profile.ID, profile.Issuer, profile.Subject); err != nil {
		failure(w, buddy.ErrForbidden)
		return
	}
	token, err := a.Sessions.CreateSession(r.Context(), profile.ID)
	if err != nil {
		failure(w, buddy.ErrUnavailable)
		return
	}
	if err := bound.BindSession(r.Context(), token, buddy.ProfileBinding(profile), principal.ExpiresAt); err != nil {
		a.Sessions.DeleteSession(r.Context(), token)
		failure(w, buddy.ErrUnavailable)
		return
	}
	if audit, ok := a.Service.Repo.(buddy.AuditStore); ok {
		if err := audit.AppendAudit(r.Context(), buddy.Event{OwnerID: profile.ID, Type: "login", ObjectID: profile.ID}); err != nil {
			a.Sessions.DeleteSession(r.Context(), token)
			failure(w, buddy.ErrUnavailable)
			return
		}
	}
	http.SetCookie(w, &http.Cookie{Name: a.sessionCookieName(), Value: token, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, Expires: principal.ExpiresAt})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
