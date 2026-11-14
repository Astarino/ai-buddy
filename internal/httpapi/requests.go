package httpapi

import (
	"github.com/Astarino/ai-buddy/internal/buddy"
	"net/http"
)

func (a *API) requestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/search", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			ID    string `json:"id"`
			Topic string `json:"topic"`
			Query string `json:"query"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := a.Service.SearchPublic(r.Context(), p, input.ID, input.Topic, input.Query)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("GET /api/requests", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		result, err := a.Service.ListRequests(r.Context(), p)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("POST /api/requests", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			ID      string `json:"id"`
			Kind    string `json:"kind"`
			Subject string `json:"subject"`
			Body    string `json:"body"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := a.Service.PrepareRequest(r.Context(), p, input.ID, input.Kind, input.Subject, input.Body)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("GET /api/requests/{id}", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		result, err := a.Service.GetRequest(r.Context(), p, r.PathValue("id"))
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("POST /api/requests/{id}/change", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			Command   string `json:"command"`
			Version   int    `json:"version"`
			Recipient string `json:"recipient"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := a.Service.ChangeRequest(r.Context(), p, r.PathValue("id"), input.Command, input.Version, input.Recipient)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("POST /api/requests/{id}/replies", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			ID      string `json:"id"`
			Text    string `json:"text"`
			Version int    `json:"version"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := a.Service.ReplyRequest(r.Context(), p, r.PathValue("id"), input.ID, input.Text, input.Version)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
	mux.HandleFunc("POST /api/requests/{id}/decision", a.withProfile(func(w http.ResponseWriter, r *http.Request, p buddy.Profile) {
		var input struct {
			Status  string `json:"status"`
			Version int    `json:"version"`
		}
		if !decode(w, r, &input) {
			return
		}
		result, err := a.Service.DecideRequest(r.Context(), p, r.PathValue("id"), input.Status, input.Version)
		if err != nil {
			failure(w, err)
			return
		}
		respond(w, result)
	}))
}
