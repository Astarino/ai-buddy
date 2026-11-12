package buddy

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"
)

const contextVersion = 1
const secretPlaceholder = "Сообщение скрыто: не отправляй пароли и токены в чат."

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
	return input, nil
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
	if message.Role != "assistant" || message.Kind == "operation" || message.Kind == "policy" {
		return true
	}
	if message.ContextVersion != contextVersion {
		// Old model output has no trustworthy record of the context that influenced it.
		return false
	}
	for _, ref := range message.Dependencies {
		doc, err := c.Document(p, granted, ref.ID)
		if err != nil || fingerprint(doc) != ref.Fingerprint {
			return false
		}
	}
	for _, id := range message.SourceIDs {
		if _, err := c.Document(p, granted, id); err != nil {
			return false
		}
	}
	return true
}

func (c Catalog) visibleMessage(p Profile, granted bool, message Message) Message {
	if message.Role == "user" && credentialRequest(message.Text) {
		message.Text = secretPlaceholder
	}
	if !c.permitsMessage(p, granted, message) {
		message.Text = "Ответ нужно перепроверить: доступ или источники изменились. Задай вопрос ещё раз."
		message.SourceIDs = []string{}
		message.ActionID = ""
		message.Kind = "restricted"
	}
	message.Dependencies = nil
	message.ContextVersion = 0
	message.RequestHash = ""
	message.Blocked = false
	return message
}

func (c Catalog) modelHistory(p Profile, granted bool, messages []Message, current string) []Message {
	history := []Message{}
	for _, old := range messages {
		if old.ID == current || old.Blocked || old.Kind == "policy" || credentialRequest(old.Text) || policyRequest(old.Text) || !c.permitsMessage(p, granted, old) {
			continue
		}
		history = append(history, old)
	}
	if len(history) > 4 {
		history = history[len(history)-4:]
	}
	return history
}
