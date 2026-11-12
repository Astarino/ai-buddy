package buddy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"slices"
	"strings"
)

const contextVersion = 1
const secretPlaceholder = "Сообщение скрыто: не отправляй пароли и токены в чат."

var credentialValue = regexp.MustCompile(`(?i)(password|пароль|api[_-]?key|токен|token)\s*[:=]\s*\S{8,}|bearer\s+[a-z0-9._-]{12,}|-----begin [a-z ]*private key|sk-proj-[a-z0-9_-]{12,}`)

func containsCredentialValue(text string) bool { return credentialValue.MatchString(text) }

func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}

func credentialRequest(text string) bool {
	return containsAny(strings.ToLower(text), "пароль", "токен", "api_key", "api-ключ", "секретный ключ", "secret", "bearer ", "password=", "password:", "-----begin private key", "sk-proj-")
}

func (input AnswerInput) Sanitized() (AnswerInput, error) {
	if credentialRequest(input.Question) || policyRequest(input.Question) {
		return AnswerInput{}, ErrForbidden
	}
	for _, doc := range input.Documents {
		if containsCredentialValue(doc.Title + " " + doc.Body) {
			return AnswerInput{}, ErrForbidden
		}
	}
	history := []Message{}
	for _, old := range input.History {
		if old.Blocked || old.Kind == "policy" || credentialRequest(old.Text) || policyRequest(old.Text) {
			continue
		}
		runes := []rune(old.Text)
		if len(runes) > 300 {
			runes = runes[:300]
		}
		history = append(history, Message{Role: old.Role, Text: string(runes)})
	}
	input.History = history
	documents := slices.Clone(input.Documents)
	for i := range documents {
		body := []rune(documents[i].Body)
		if len(body) > 900 {
			documents[i].Body = string(body[:900])
		}
		documents[i].Keywords = ""
	}
	input.Documents = documents
	return input, nil
}

func (input AnswerInput) PublicOnly() bool {
	for _, doc := range input.Documents {
		if doc.Scope != "public" || doc.DataClass != "public" || credentialRequest(doc.Title+" "+doc.Body) {
			return false
		}
	}
	return true
}

func policyRequest(text string) bool {
	return containsAny(strings.ToLower(text), "игнорируй", "зарплат", "чужой команд", "другой команд", "я админ", "я руководитель", "согласуй", "выдай мне", "system prompt", "системный промпт")
}

func contextRefs(docs []Document, history []Message) []DocumentRef {
	refs := []DocumentRef{}
	for _, doc := range docs {
		refs = append(refs, DocumentRef{doc.ID, fingerprint(doc)})
	}
	for _, message := range history {
		for _, ref := range message.Dependencies {
			if !slices.Contains(refs, ref) {
				refs = append(refs, ref)
			}
		}
	}
	return refs
}

func (c Catalog) permitsMessage(p Profile, granted bool, message Message) bool {
	return permitsMessage(c.Allowed(p, granted), message)
}

func permitsMessage(documents []Document, message Message) bool {
	docs := map[string]Document{}
	for _, d := range documents {
		docs[d.ID] = d
	}
	if message.Role != "assistant" || message.Kind == "operation" || message.Kind == "policy" {
		return true
	}
	if message.ContextVersion != contextVersion {
		// Old model output has no trustworthy record of the context that influenced it.
		return false
	}
	for _, ref := range message.Dependencies {
		doc, ok := docs[ref.ID]
		if !ok || fingerprint(doc) != ref.Fingerprint {
			return false
		}
	}
	for _, id := range message.SourceIDs {
		if _, ok := docs[id]; !ok {
			return false
		}
	}
	return true
}

func (c Catalog) visibleMessage(p Profile, granted bool, message Message) Message {
	return visibleMessage(c.Allowed(p, granted), message)
}

func visibleMessage(documents []Document, message Message) Message {
	if message.Role == "user" && credentialRequest(message.Text) {
		message.Text = secretPlaceholder
	}
	if !permitsMessage(documents, message) {
		message.Text = "Ответ нужно перепроверить: доступ или источники изменились. Задай вопрос ещё раз."
		message.SourceIDs = []string{}
		message.ActionID = ""
		message.Kind = "restricted"
	}
	message.Dependencies = nil
	message.ContextVersion = 0
	message.RequestHash = ""
	message.ConsentTo = ""
	message.Blocked = false
	return message
}

func (c Catalog) modelHistory(p Profile, granted bool, messages []Message, current string) []Message {
	return modelHistory(c.Allowed(p, granted), messages, current)
}

func modelHistory(documents []Document, messages []Message, current string) []Message {
	history := []Message{}
	for _, old := range messages {
		if old.ID == current || old.Blocked || old.Kind == "policy" || credentialRequest(old.Text) || policyRequest(old.Text) || !permitsMessage(documents, old) {
			continue
		}
		history = append(history, old)
	}
	if len(history) > 4 {
		history = history[len(history)-4:]
	}
	return history
}
