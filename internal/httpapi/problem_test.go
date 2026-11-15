package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Astarino/ai-buddy/internal/buddy"
	"github.com/Astarino/ai-buddy/internal/model"
	"github.com/Astarino/ai-buddy/internal/publicdocs"
)

// This test uses actual public documents and inference, and executes only the
// fixed Go test command below. The model never receives a shell tool.
func TestRealGoProblem(t *testing.T) {
	if os.Getenv("RUN_MODEL_PROBLEM") != "true" {
		t.Skip("set RUN_MODEL_PROBLEM=true for live documentation and Qwen")
	}
	b, api := apiWorld(t)
	base := os.Getenv("EVAL_MODEL_BASE_URL")
	if base == "" {
		base = "http://127.0.0.1:11434"
	}
	llm, err := model.New("ollama", base, "qwen3.5:4b", "", false)
	if err != nil {
		t.Fatal(err)
	}
	client := llm.(*model.Client)
	client.CPUOnly = os.Getenv("EVAL_MODEL_CPU_ONLY") == "true"
	client.KeepAliveSeconds, _ = strconv.Atoi(os.Getenv("EVAL_KEEP_ALIVE_SECONDS"))
	api.Service.Model = llm
	api.Service.Searcher = publicdocs.New()

	report := map[string]any{
		"model": llm.Name(), "prompt_version": model.PromptVersion,
		"generated_at":          time.Now().UTC().Format(time.RFC3339),
		"execution_environment": os.Getenv("EVAL_ENVIRONMENT"),
		"passed":                false,
		"scope":                 "Synthetic reconstruction of research I-01. Actual HTTP search and Qwen answer; the tester manually applies a fixed command in an isolated Go module. No model shell execution or human interview.",
	}
	defer func() {
		encoded, e := json.MarshalIndent(report, "", "  ")
		if e == nil {
			e = os.WriteFile("../../reports/model-problem.json", append(encoded, '\n'), 0644)
		}
		if e != nil {
			t.Error(e)
		}
	}()
	if commit, e := exec.Command("git", "rev-parse", "HEAD").Output(); e == nil {
		report["application_commit"] = strings.TrimSpace(string(commit))
	}

	workspace := t.TempDir()
	project := filepath.Join(workspace, "hello")
	if err := os.Mkdir(project, 0755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"go.mod":        "module example.test/hello\n\ngo 1.25.5\n",
		"hello.go":      "package hello\n\nfunc Greeting(name string) string { return \"Привет, \" + name }\n",
		"hello_test.go": "package hello\n\nimport \"testing\"\n\nfunc TestGreeting(t *testing.T) { if Greeting(\"Антон\") != \"Привет, Антон\" { t.Fatal(\"wrong greeting\") } }\n",
	}
	for name, text := range files {
		if err := os.WriteFile(filepath.Join(project, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	report["project_files"] = files
	run := func(dir string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "go", "test", "./...")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GOWORK=off", "GO111MODULE=on", "GOTOOLCHAIN=local")
		out, e := cmd.CombinedOutput()
		return string(out), e
	}
	before, beforeErr := run(workspace)
	report["before"] = map[string]any{"directory": "workspace", "command": "go test ./...", "output": before, "failed": beforeErr != nil}
	if beforeErr == nil || !strings.Contains(before, "does not contain main module") {
		t.Fatal("expected the real module-root error", before, beforeErr)
	}
	b.expect("POST", "/api/session", `{"profile_id":"anton"}`, 200)
	clarification := readJSON[buddy.Message](t, b.expect("POST", "/api/messages", `{"id":"problem-clarify","text":"Не запускаются тесты. Не понимаю, что пропустил."}`, 200))
	report["clarification"] = clarification
	if clarification.Kind != "clarification" {
		t.Fatal("an incomplete problem needs clarification", clarification)
	}
	question := "Запускаю go test ./... из workspace. Проект лежит в workspace/hello, там есть go.mod. Ошибка: directory prefix . does not contain main module or its selected dependencies. Как продолжить?"
	searchBody, _ := json.Marshal(map[string]string{"id": "problem-search", "topic": "go", "query": question})
	search := readJSON[buddy.PublicSearch](t, b.expect("POST", "/api/search", string(searchBody), 200))
	report["search"] = search
	if len(search.Documents) == 0 {
		t.Fatal("no official source found")
	}
	for _, doc := range search.Documents {
		b.expect("GET", "/api/documents/"+doc.ID, "", 200)
	}
	body, _ := json.Marshal(map[string]string{"id": "problem-answer", "text": question})
	started := time.Now()
	answer := readJSON[buddy.Message](t, b.expect("POST", "/api/messages", string(body), 200))
	report["elapsed_ms"] = time.Since(started).Milliseconds()
	report["first_answer"] = answer
	report["model_responses"] = 1
	report["real_inference"] = answer.Kind == "answer" && len(answer.SourceIDs) > 0
	lower := strings.ToLower(answer.Text)
	if strings.Contains(lower, "без точки") || strings.Contains(lower, "go mod init") {
		t.Fatal("advice contradicts the already existing module", answer)
	}
	if !strings.Contains(lower, "hello") {
		// A sourced general rule can be useful, but it is not yet a usable step.
		// Keep it visible and ask for the concrete next step instead of counting it as success.
		followup := "В какую именно папку мне перейти? Сейчас я в workspace, а go.mod лежит в workspace/hello."
		report["followup_question"] = followup
		body, _ = json.Marshal(map[string]string{"id": "problem-followup", "text": followup})
		answer = readJSON[buddy.Message](t, b.expect("POST", "/api/messages", string(body), 200))
		report["model_responses"] = 2
		lower = strings.ToLower(answer.Text)
	}
	report["elapsed_ms"] = time.Since(started).Milliseconds()
	report["answer"] = answer
	if answer.Kind != "answer" || len(answer.SourceIDs) == 0 || !strings.Contains(lower, "hello") || !(strings.Contains(lower, "каталог") || strings.Contains(lower, "папк") || strings.Contains(lower, "директори") || strings.Contains(lower, "cd ")) {
		t.Fatal("answer did not identify the existing module directory", answer)
	}
	if strings.Contains(lower, "go mod init") || strings.Contains(lower, "без точки") {
		t.Fatal("unsafe or irrelevant change to the existing module", answer)
	}
	for _, source := range answer.SourceIDs {
		found := false
		for _, doc := range search.Documents {
			found = found || source == doc.ID
		}
		if !found {
			t.Fatal("answer did not cite the fetched search", source)
		}
	}
	after, afterErr := run(project)
	report["after"] = map[string]any{"directory": "workspace/hello", "command": "go test ./...", "output": after, "passed": afterErr == nil}
	if afterErr != nil {
		t.Fatal("fixed command failed", after, afterErr)
	}
	profile, _ := api.Service.Catalog.Profile("anton")
	state, err := api.Service.Repo.Read(context.Background(), profile.ID)
	if err != nil {
		t.Fatal(err)
	}
	report["saved_state"] = state
	report["passed"] = true
	t.Log(fmt.Sprintf("Real Qwen: %s\nBefore: %s\nAfter: %s", answer.Text, before, after))
}
