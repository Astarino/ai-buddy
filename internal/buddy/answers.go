package buddy

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

var projectCode = regexp.MustCompile(`(?i)проект\s+Jira\s*:\s*([A-Z][A-Z0-9_-]{1,40})`)
var missingInformation = regexp.MustCompile(`(?i)нет (информации|сведений|точных|достоверн|доступа к URL)|не содерж(ит|ат).{0,70}(информац|инструкц|команд)|точн(ой|ая|ых) команд.{0,70}нет|в (документе|документах|источниках).{0,90}нет`)

func vagueProblem(query string) bool {
	if !containsAny(query, "ошиб", "настрой", "не понима", "помог", "не получ", "сломал", "проблем") {
		return false
	}
	stop := " не понимаю эту этот это что мне сделать с как в вот на почему работает работать запустить получается какая какой какие помощь "
	for _, word := range words(query) {
		if strings.Contains(stop, " "+word+" ") || containsPrefix(word, "ошиб", "настро", "разобр", "помог", "исправ", "проблем", "сломал") {
			continue
		}
		return false
	}
	return true
}

func containsPrefix(word string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(word, prefix) {
			return true
		}
	}
	return false
}

func conflictingSources(query string) bool {
	return containsAny(query, "документ", "источник", "инструкц", "правил") && containsAny(query, "противореч", "разные сроки", "разные требован", "сведения расход", "документы расход")
}

func sourceReply(p Profile, query string, docs []Document) (Answer, bool) {
	if strings.Contains(query, "jira") && strings.Contains(query, "проект") && containsAny(query, "ключ", "код") {
		for _, doc := range docs {
			if doc.Team != p.Team {
				continue
			}
			match := projectCode.FindStringSubmatch(doc.Body)
			if len(match) != 2 {
				continue
			}
			prefix := "В описании твоей команды проект Jira: "
			if doc.Synthetic {
				prefix = "В тестовом описании твоей команды проект Jira: "
			}
			return Answer{Text: prefix + match[1] + ".", SourceIDs: []string{doc.ID}}, true
		}
	}
	if !containsAny(query, "чем полез", "что описывает", "что описано") {
		return Answer{}, false
	}
	for _, doc := range docs {
		if doc.Scope != "public" {
			continue
		}
		named := false
		for _, word := range words(doc.Title + " " + doc.Keywords) {
			if utf8.RuneCountInString(word) >= 5 && !containsPrefix(word, "публич", "тестов", "команд", "разраб", "материал", "принцип") && strings.Contains(query, word) {
				named = true
			}
		}
		if !named {
			continue
		}
		parts := strings.SplitAfter(doc.Body, ". ")
		if len(parts) > 2 {
			parts = parts[:2]
		}
		text := strings.TrimSpace(strings.Join(parts, ""))
		if runes := []rune(text); len(runes) > 550 {
			text = strings.TrimSpace(string(runes[:550])) + "…"
		}
		return Answer{Text: "В источнике:\n«" + text + "»", SourceIDs: []string{doc.ID}}, true
	}
	return Answer{}, false
}
