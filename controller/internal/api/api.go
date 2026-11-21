// Package api is what the AI calls. The contract is architecture.md and the
// design is in design.md.
package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kdop/gangway/controller/internal/relay"
	"github.com/kdop/gangway/controller/internal/sessions"
	"github.com/kdop/gangway/shared/messages"
)

// maxBody is the largest request body the API reads. A request must still
// fit in one message to the host.
const maxBody = messages.MaxMessageBytes / 2

// New gives the handler for every path the door for the hosts does not
// take.
func New(store *sessions.Store) http.Handler {
	a := &api{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/hello", a.auth(a.session))
	mux.HandleFunc("GET /v1/session", a.auth(a.session))
	mux.HandleFunc("POST /v1/requests", a.auth(a.requests))
	mux.HandleFunc("POST /v1/requests/{id}/wait", a.auth(a.wait))
	mux.HandleFunc("POST /v1/requests/{id}/stop", a.auth(a.stop))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found")
	})
	return mux
}

type api struct {
	store *sessions.Store
}

type handler func(http.ResponseWriter, *http.Request, *sessions.Session)

// auth lets a call in only with the key of a live session. Every refusal
// looks the same, so a guesser learns nothing from it.
func (a *api) auth(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		var sess *sessions.Session
		if ok {
			sess = a.store.Find(key)
		}
		if sess == nil {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		next(w, r, sess)
	}
}

type limitsBody struct {
	CommandSeconds int `json:"command_seconds"`
	AnswerSeconds  int `json:"answer_seconds"`
}

type requestsBody struct {
	Waiting    int     `json:"waiting"`
	MaxWaiting int     `json:"max_waiting"`
	Running    *string `json:"running"`
}

type sessionBody struct {
	Host     string       `json:"host"`
	Limits   limitsBody   `json:"limits"`
	Requests requestsBody `json:"requests"`
}

func (a *api) session(w http.ResponseWriter, r *http.Request, sess *sessions.Session) {
	waiting, running := sess.Relay.Counts()
	body := sessionBody{
		Host:     sess.Host,
		Limits:   limitsBody{sess.Limits.CommandSeconds, sess.Limits.AnswerSeconds},
		Requests: requestsBody{Waiting: waiting, MaxWaiting: sess.Limits.MaxWaiting},
	}
	if running != "" {
		body.Requests.Running = &running
	}
	writeJSON(w, http.StatusOK, body)
}

func (a *api) requests(w http.ResponseWriter, r *http.Request, sess *sessions.Session) {
	var body struct {
		Kind    string `json:"kind"`
		Command string `json:"command"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	if body.Kind != messages.KindCommand || body.Command == "" {
		writeError(w, http.StatusBadRequest, "bad_request")
		return
	}
	// Only the command goes on. Whatever else the body held is left behind.
	id, call := sess.Relay.Ask(body.Command)
	a.answer(w, r, sess, id, call)
}

func (a *api) wait(w http.ResponseWriter, r *http.Request, sess *sessions.Session) {
	a.reopen(w, r, sess, sess.Relay.Wait)
}

func (a *api) stop(w http.ResponseWriter, r *http.Request, sess *sessions.Session) {
	a.reopen(w, r, sess, sess.Relay.Stop)
}

func (a *api) reopen(w http.ResponseWriter, r *http.Request, sess *sessions.Session, open func(string) (relay.Call, bool)) {
	id := r.PathValue("id")
	call, ok := open(id)
	if !ok {
		writeError(w, http.StatusNotFound, "not_found")
		return
	}
	a.answer(w, r, sess, id, call)
}

// answer keeps the call open until the relay has an answer or the AI hangs
// up.
func (a *api) answer(w http.ResponseWriter, r *http.Request, sess *sessions.Session, id string, call relay.Call) {
	select {
	case ans := <-call:
		writeJSON(w, http.StatusOK, answerBody(ans, sess))
	case <-r.Context().Done():
		sess.Relay.HangUp(id, call)
	}
}

func answerBody(ans relay.Answer, sess *sessions.Session) any {
	switch ans.State {
	case relay.StateRefused:
		return struct {
			State  string `json:"state"`
			Reason string `json:"reason"`
		}{ans.State, ans.Reason}
	case relay.StateRunning:
		return struct {
			State  string `json:"state"`
			ID     string `json:"id"`
			Output string `json:"output"`
			Within int    `json:"answer_within_seconds"`
		}{ans.State, ans.ID, ans.Output, sess.Limits.AnswerSeconds}
	}
	return struct {
		State         string `json:"state"`
		How           string `json:"how"`
		ExitCode      *int   `json:"exit_code,omitempty"`
		Output        string `json:"output"`
		Cut           bool   `json:"cut"`
		BytesReplaced bool   `json:"bytes_replaced"`
	}{ans.State, ans.How, ans.ExitCode, ans.Output, ans.Cut, ans.BytesReplaced}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
