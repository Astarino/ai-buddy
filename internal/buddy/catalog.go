package buddy

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

//go:embed corpus.json
var corpus []byte

type Catalog struct {
	Profiles  []Profile  `json:"profiles"`
	Documents []Document `json:"documents"`
	Tasks     []Task     `json:"tasks"`
}

func LoadCatalog() Catalog {
	var c Catalog
	if err := json.Unmarshal(corpus, &c); err != nil {
		panic(err)
	}
	return c
}

func (c Catalog) Profile(id string) (Profile, error) {
	for _, p := range c.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, ErrNotFound
}

func HasAccess(ops []Operation) bool {
	for _, op := range ops {
		if op.Kind == "access" && op.Status == "granted" {
			return true
		}
	}
	return false
}

func (c Catalog) Allowed(p Profile, granted bool) []Document {
	docs := []Document{}
	for _, d := range c.Eligible(p) {
		if d.Scope == "space" && (!granted || d.Resource == "" || d.Resource != p.Space || d.Team != p.Team) {
			continue
		}
		docs = append(docs, d)
	}
	return docs
}

func (c Catalog) Eligible(p Profile) []Document {
	docs := []Document{}
	for _, d := range c.Documents {
		if containsCredentialValue(d.Title + " " + d.Body) {
			continue
		}
		if d.Team != "" && d.Team != p.Team {
			continue
		}
		if d.Direction != "" && d.Direction != p.Direction {
			continue
		}
		switch d.Scope {
		case "public", "team", "space":
		default:
			continue
		}
		docs = append(docs, d)
	}
	return docs
}

func (c Catalog) Document(p Profile, granted bool, id string) (Document, error) {
	for _, d := range c.Allowed(p, granted) {
		if d.ID == id {
			return d, nil
		}
	}
	return Document{}, ErrNotFound
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(strings.ReplaceAll(s, "ё", "е")), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

func Search(docs []Document, query string) []Document {
	tokens := words(query)
	queryLower := strings.ToLower(query)
	requestedChecklist := ""
	if strings.Contains(queryLower, "чек") {
		general := strings.Contains(queryLower, "общ")
		backend := strings.Contains(queryLower, "backend") || strings.Contains(queryLower, "бэкенд")
		if general && !backend {
			requestedChecklist = "checklist"
		}
		if backend && !general {
			requestedChecklist = "backend-checklist"
		}
	}
	if requestedChecklist != "" {
		for _, doc := range docs {
			if doc.ID == requestedChecklist {
				return []Document{doc}
			}
		}
		return []Document{}
	}
	type match struct {
		doc   Document
		score int
	}
	matches := []match{}
	for _, d := range docs {
		score := 0
		title := strings.ToLower(d.Title + " " + d.Keywords)
		body := strings.ToLower(d.Body)
		for _, token := range tokens {
			if len([]rune(token)) < 3 {
				continue
			}
			// A short prefix covers Russian inflections without a separate NLP model.
			stem := []rune(token)
			if len(stem) > 5 {
				stem = stem[:len(stem)-2]
			}
			if strings.Contains(title, string(stem)) {
				score += 4
			}
			if strings.Contains(body, string(stem)) {
				score++
			}
		}
		if score > 0 {
			matches = append(matches, match{d, score})
		}
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].score > matches[j].score })
	result := []Document{}
	for i, m := range matches {
		if i == 3 {
			break
		}
		result = append(result, m.doc)
	}
	return result
}

func (c Catalog) Route(p Profile, state State, ops []Operation) ([]Task, string) {
	tasks := []Task{}
	done := map[string]bool{}
	for k, v := range state.Completed {
		done[k] = v
	}
	if p.Issuer == "" {
		for _, id := range p.InitialCompleted {
			done[id] = true
		}
	}
	done["space-access"] = HasAccess(ops)
	for _, op := range ops {
		if op.Kind == "issue" && (op.Status == "assigned" || op.Status == "done") {
			done["first-issue"] = true
		}
		if op.Kind == "issue" && op.Status == "done" {
			done["finish-issue"] = true
		}
	}
	next := ""
	for _, base := range c.Tasks {
		if base.Direction != "" && base.Direction != p.Direction {
			continue
		}
		t := base
		if t.DocumentID == "team-guide" {
			t.DocumentID = p.Team + "-guide"
		}
		t.Status = "available"
		for _, dep := range t.Requires {
			if !done[dep] {
				t.Status = "blocked"
				t.Blocker = dep
				break
			}
		}
		if done[t.ID] && t.Status != "blocked" {
			t.Status = "completed"
		}
		if t.ID == "space-access" && !done[t.ID] {
			for _, op := range ops {
				if op.Kind == "access" && (op.Status == "pending" || op.Status == "approved") {
					t.Status = "waiting"
				}
			}
		}
		if t.ID == "finish-issue" && done["first-issue"] && !done[t.ID] {
			t.Status = "in_progress"
		}
		if next == "" && t.Status == "available" {
			next = t.ID
		}
		tasks = append(tasks, t)
	}
	return tasks, next
}
