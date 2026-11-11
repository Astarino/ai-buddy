package buddy

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrForbidden   = errors.New("forbidden")
	ErrConflict    = errors.New("conflict")
	ErrInvalid     = errors.New("invalid input")
	ErrExpired     = errors.New("confirmation expired")
	ErrUnavailable = errors.New("service unavailable")
	ErrBusy        = errors.New("generation busy")
)

type Profile struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Team     string `json:"team"`
	TeamName string `json:"team_name"`
	Role     string `json:"role"`
	Mentor   string `json:"mentor"`
	Day      int    `json:"day"`
	Space    string `json:"space"`
	Project  string `json:"project"`
}

type Document struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Body      string `json:"body"`
	Team      string `json:"team"`
	Scope     string `json:"scope"`
	Origin    string `json:"origin"`
	URL       string `json:"url,omitempty"`
	Version   string `json:"version"`
	UpdatedAt string `json:"updated_at"`
	Synthetic bool   `json:"synthetic"`
	Keywords  string `json:"keywords"`
}

type Task struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Period      string   `json:"period"`
	Owner       string   `json:"owner"`
	Requires    []string `json:"requires"`
	Completion  string   `json:"completion"`
	DocumentID  string   `json:"document_id"`
	Source      string   `json:"source"`
	SourceURL   string   `json:"source_url"`
	Status      string   `json:"status"`
	Blocker     string   `json:"blocker,omitempty"`
}

type Message struct {
	ID         string    `json:"id"`
	Role       string    `json:"role"`
	Text       string    `json:"text"`
	CreatedAt  time.Time `json:"created_at"`
	SourceIDs  []string  `json:"source_ids"`
	ActionID   string    `json:"action_id,omitempty"`
	ReplyTo    string    `json:"reply_to,omitempty"`
	Kind       string    `json:"kind,omitempty"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

type Action struct {
	ID        string    `json:"id"`
	Kind      string    `json:"kind"`
	Resource  string    `json:"resource"`
	Level     string    `json:"level"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	ExpiresAt time.Time `json:"expires_at"`
	ResultID  string    `json:"result_id,omitempty"`
}

type Operation struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	UserID    string    `json:"user_id"`
	Kind      string    `json:"kind"`
	Resource  string    `json:"resource"`
	Level     string    `json:"level"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	Note      string    `json:"note"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Event struct {
	Type       string    `json:"type"`
	ObjectID   string    `json:"object_id"`
	At         time.Time `json:"at"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

type State struct {
	Messages  []Message       `json:"messages"`
	Actions   []Action        `json:"actions"`
	Completed map[string]bool `json:"completed"`
	Events    []Event         `json:"events"`
	Draft     string          `json:"draft"`
}

func EmptyState() State {
	return State{Messages: []Message{}, Actions: []Action{}, Completed: map[string]bool{"meet-team": true}, Events: []Event{}}
}

type View struct {
	Profile      Profile     `json:"profile"`
	Messages     []Message   `json:"messages"`
	Actions      []Action    `json:"actions"`
	Tasks        []Task      `json:"tasks"`
	Operations   []Operation `json:"operations"`
	Documents    []Document  `json:"documents"`
	NextTaskID   string      `json:"next_task_id"`
	Draft        string      `json:"draft"`
	ServiceFresh bool        `json:"service_fresh"`
	Model        string      `json:"model"`
	Demo         bool        `json:"demo"`
}

type Repository interface {
	Read(context.Context, string) (State, error)
	Update(context.Context, string, func(*State) error) (State, error)
}

type Corporate interface {
	List(context.Context, string) ([]Operation, error)
	Execute(context.Context, string, Action) (Operation, error)
	Event(context.Context, string, string, string) error
}

type AnswerInput struct {
	Question  string     `json:"question"`
	Team      string     `json:"team"`
	Day       int        `json:"day"`
	Documents []Document `json:"documents"`
	History   []Message  `json:"history"`
}

type Answer struct {
	Text               string   `json:"answer"`
	SourceIDs          []string `json:"source_ids"`
	NeedsClarification bool     `json:"needs_clarification"`
}

type Model interface {
	Answer(context.Context, AnswerInput) (Answer, error)
	Name() string
}
