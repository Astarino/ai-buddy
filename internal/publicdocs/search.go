package publicdocs

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type entry struct{ title, url, raw string }

var sources = map[string][]entry{
	"go": {
		{"Создать модуль Go", "https://go.dev/doc/tutorial/create-module", "https://raw.githubusercontent.com/golang/website/master/_content/doc/tutorial/create-module.html"},
		{"Тесты в Go", "https://go.dev/doc/tutorial/add-a-test", "https://raw.githubusercontent.com/golang/website/master/_content/doc/tutorial/add-a-test.html"},
		{"Go Modules Reference", "https://go.dev/ref/mod", "https://raw.githubusercontent.com/golang/website/master/_content/ref/mod.md"},
	},
	"git": {
		{"git status", "https://git-scm.com/docs/git-status", "https://raw.githubusercontent.com/git/git/master/Documentation/git-status.adoc"},
		{"git clone", "https://git-scm.com/docs/git-clone", "https://raw.githubusercontent.com/git/git/master/Documentation/git-clone.adoc"},
		{"git restore", "https://git-scm.com/docs/git-restore", "https://raw.githubusercontent.com/git/git/master/Documentation/git-restore.adoc"},
	},
}

type Client struct{ HTTP *http.Client }

func New() *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil || host != "raw.githubusercontent.com" || port != "443" {
			return nil, fmt.Errorf("unapproved documentation host")
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if !publicIP(ip.IP) {
				return nil, fmt.Errorf("documentation host resolved to a non-public address")
			}
		}
		var last error
		for _, ip := range ips {
			conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
			if err == nil {
				return conn, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("no documentation addresses")
		}
		return nil, last
	}
	return &Client{HTTP: &http.Client{Timeout: 8 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func publicIP(ip net.IP) bool {
	return ip.IsGlobalUnicast() && !ip.IsPrivate() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() && !ip.IsUnspecified() && !net.IPv4(100, 64, 0, 0).Equal(ip) && !(ip.To4() != nil && ip.To4()[0] == 100 && ip.To4()[1] >= 64 && ip.To4()[1] <= 127)
}

func (c *Client) Search(ctx context.Context, topic, query string) ([]buddy.Document, error) {
	entries, ok := sources[topic]
	if !ok || len(query) > 8000 {
		return nil, buddy.ErrInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	result := []buddy.Document{}
	for _, source := range entries {
		body, err := c.fetch(ctx, source.raw)
		if err != nil {
			return nil, buddy.ErrUnavailable
		}
		fragment := excerpt(body, query)
		if fragment == "" {
			continue
		}
		digest := sha256.Sum256([]byte(body))
		result = append(result, buddy.Document{Title: source.title, Body: fragment, Scope: "public", DataClass: "public", Origin: "Официальная документация; свежий снимок из " + source.raw, URL: source.url, Version: hex.EncodeToString(digest[:]), UpdatedAt: time.Now().UTC().Format(time.RFC3339), Keywords: topic})
	}
	// Prefer the definition of a module root over instructions for starting a new module.
	if strings.Contains(strings.ToLower(query), "go.mod") || strings.Contains(query, "directory prefix") {
		sort.SliceStable(result, func(i, j int) bool {
			return strings.Contains(result[i].Body, "module root directory") && !strings.Contains(result[j].Body, "module root directory")
		})
	}
	return result, nil
}

func (c *Client) fetch(ctx context.Context, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "raw.githubusercontent.com" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", buddy.ErrForbidden
	}
	known := false
	for _, list := range sources {
		for _, source := range list {
			if source.raw == raw {
				known = true
			}
		}
	}
	if !known {
		return "", buddy.ErrForbidden
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "text/plain")
	req.Header.Set("User-Agent", "AI-Buddy-documentation/1.0")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return "", buddy.ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(body) > 1<<20 {
		return "", buddy.ErrUnavailable
	}
	return string(body), nil
}

var tags = regexp.MustCompile(`<[^>]*>`)

func excerpt(body, query string) string {
	// This extracts plain search text; no fetched markup is ever rendered as HTML.
	body = strings.NewReplacer("</p>", "\n\n", "</pre>", "\n\n", "</li>", "\n\n", "</h2>", "\n\n", "</h3>", "\n\n").Replace(body)
	query = strings.ToLower(query)
	terms := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '.' })
	for _, rule := range []struct{ match, words string }{
		{"тест", "test testing"}, {"модул", "module go.mod directory"}, {"папк", "directory root go.mod"}, {"каталог", "directory root"}, {"pattern", "directory module root go.mod"}, {"go.mod", "module directory root"},
		{"изменен", "status modified"}, {"изменён", "status modified"}, {"отмен", "restore working tree"}, {"клонир", "clone repository"}, {"репозитор", "repository"},
	} {
		if strings.Contains(query, rule.match) {
			terms = append(terms, strings.Fields(rule.words)...)
		}
	}
	type chunk struct {
		text  string
		score int
		order int
	}
	chunks := []chunk{}
	for i, p := range strings.Split(body, "\n\n") {
		p = strings.TrimSpace(html.UnescapeString(tags.ReplaceAllString(p, "")))
		if p == "" || strings.HasPrefix(p, "---") || strings.HasPrefix(p, "{{") {
			continue
		}
		score := 0
		lower := strings.ToLower(p)
		if strings.Contains(query, "go.mod") && (strings.Contains(query, "director") || strings.Contains(query, "папк") || strings.Contains(query, "корн")) && strings.Contains(lower, "module root directory") {
			score += 15
		}
		for _, term := range terms {
			if len([]rune(term)) >= 3 && strings.Contains(lower, term) {
				score++
			}
		}
		if score > 0 {
			chunks = append(chunks, chunk{p, score, i})
		}
	}
	sort.SliceStable(chunks, func(i, j int) bool { return chunks[i].score > chunks[j].score })
	selected := []chunk{}
	for _, c := range chunks {
		if len(selected) == 3 {
			break
		}
		if len([]rune(c.text)) > 600 {
			c.text = string([]rune(c.text)[:600])
		}
		selected = append(selected, c)
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].order < selected[j].order })
	parts := []string{}
	for _, c := range selected {
		parts = append(parts, c.text)
	}
	text := strings.Join(parts, "\n\n")
	if len([]rune(text)) > 900 {
		text = string([]rune(text)[:900])
	}
	return text
}
