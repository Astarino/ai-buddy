package buddy

import (
	"strings"
	"testing"
)

func TestCatalogRetrievalAndDependencies(t *testing.T) {
	c := LoadCatalog()
	p, err := c.Profile("anton")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = c.Profile("missing"); err != ErrNotFound {
		t.Fatal(err)
	}
	for _, granted := range []bool{false, true} {
		allowed := c.Allowed(p, granted)
		for _, doc := range allowed {
			if doc.Scope == "mentor" || strings.Contains(doc.ID, "delivery") || (!granted && doc.Scope == "space") {
				t.Fatal(doc)
			}
		}
		for _, q := range []string{"как запросить доступ", "наставник", "первые две недели", "123 отсутствующее-слово", "x"} {
			results := Search(allowed, q)
			if len(results) > 3 {
				t.Fatal(results)
			}
			for _, doc := range results {
				if strings.Contains(doc.Body, "ACL_CANARY") {
					t.Fatal("private evidence leaked")
				}
			}
		}
	}
	state := EmptyState()
	state.Completed["engineering"] = true
	ops := []Operation{{Kind: "access", Status: "approved"}}
	tasks, next := c.Route(p, state, ops)
	if len(tasks) != 12 || next == "" {
		t.Fatal(tasks, next)
	}
	ops[0].Status = "granted"
	state.Completed["read-guide"] = true
	ops = append(ops, Operation{Kind: "issue", Status: "done"})
	tasks, _ = c.Route(p, state, ops)
	for _, task := range tasks {
		if task.ID == "finish-issue" && task.Status != "completed" {
			t.Fatal(task)
		}
		if task.ID == "read-guide" && task.DocumentID != "catalog-guide" {
			t.Fatal(task)
		}
	}
	if HasAccess(nil) {
		t.Fatal("empty grant")
	}
}

func TestExplicitChecklistScopeOutranksTheOtherList(t *testing.T) {
	c := LoadCatalog()
	p, _ := c.Profile("anton")
	allowed := c.Allowed(p, false)
	for question, expected := range map[string]string{
		"Что относится ко второй неделе в общем чек-листе?":  "checklist",
		"Кто добавляет стажёра в чаты по общему чек-листу?":  "checklist",
		"Что изучают по Backend чек-листу на второй неделе?": "backend-checklist",
	} {
		results := Search(allowed, question)
		if len(results) == 0 || results[0].ID != expected {
			t.Fatal(question, results)
		}
	}
}
