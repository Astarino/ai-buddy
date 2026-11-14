package publicdocs

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"

	"github.com/Astarino/ai-buddy/internal/buddy"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLiveFetchRankingAndEgress(t *testing.T) {
	calls := 0
	c := New()
	c.HTTP.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.RawQuery != "" || r.Body != nil || r.URL.Host != "raw.githubusercontent.com" || r.Header.Get("Authorization") != "" {
			t.Fatal("question or credential escaped", r)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("Unrelated paragraph.\n\nThe module root directory contains a go.mod file. Run go test ./... from that directory.\n\nOther material."))}, nil
	})
	docs, err := c.Search(context.Background(), "go", "go test ./... go.mod папка")
	if err != nil || calls != 3 || len(docs) != 3 {
		t.Fatal(docs, calls, err)
	}
	if !strings.Contains(docs[2].Body, "module root") || len(docs[2].Version) != 64 || docs[0].Synthetic || docs[0].DataClass != "public" {
		t.Fatal(docs)
	}
	docs, err = c.Search(context.Background(), "git", "неизвестноничего")
	if err != nil || len(docs) != 0 {
		t.Fatal(docs, err)
	}
	if _, err = c.Search(context.Background(), "anything", "x"); !errors.Is(err, buddy.ErrInvalid) {
		t.Fatal(err)
	}
	if excerpt("<p>go &amp; test</p>", "test") != "go & test" {
		t.Fatal("plain HTML extraction")
	}
	root := excerpt("A module is identified by a module path. The module root directory is the directory that contains the go.mod file.\n\n"+strings.Repeat("go.mod directory prefix module command test ", 80)+"\n\nMore directory modules.", "go test ./...: directory prefix does not contain main module go.mod")
	if !strings.Contains(root, "module root directory") {
		t.Fatal("module-root definition lost", root)
	}
	if len([]rune(excerpt(strings.Repeat("module ", 500), "module"))) > 900 {
		t.Fatal("unbounded snippet")
	}
}

func TestSearchRejectsUnsafeFetchesAndFailures(t *testing.T) {
	c := New()
	for _, url := range []string{"http://raw.githubusercontent.com/x", "https://localhost/", "https://raw.githubusercontent.com/unknown", "https://raw.githubusercontent.com/golang/website/master/_content/ref/mod.md?q=private"} {
		if _, err := c.fetch(context.Background(), url); !errors.Is(err, buddy.ErrForbidden) {
			t.Fatal(url, err)
		}
	}
	for _, tc := range []struct {
		status int
		body   string
		err    error
	}{{302, "", nil}, {503, "", nil}, {200, strings.Repeat("x", (1<<20)+1), nil}, {0, "", errors.New("offline")}} {
		c.HTTP.Transport = roundTrip(func(*http.Request) (*http.Response, error) {
			if tc.err != nil {
				return nil, tc.err
			}
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
		})
		if _, err := c.Search(context.Background(), "go", "module"); !errors.Is(err, buddy.ErrUnavailable) {
			t.Fatal(err)
		}
	}
	c = New()
	transport := c.HTTP.Transport.(*http.Transport)
	if transport.Proxy != nil {
		t.Fatal("environment proxy enabled")
	}
	if _, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:443"); err == nil {
		t.Fatal("loopback permitted")
	}
	if c.HTTP.CheckRedirect(nil, nil) != http.ErrUseLastResponse {
		t.Fatal("redirect enabled")
	}
	for _, raw := range []string{"127.0.0.1", "10.1.2.3", "169.254.169.254", "::1", "100.64.1.1", "192.168.1.4"} {
		if publicIP(net.ParseIP(raw)) {
			t.Fatal(raw)
		}
	}
	if !publicIP(net.ParseIP("185.199.108.133")) {
		t.Fatal("public address denied")
	}
}
