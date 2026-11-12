package outbound

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDestinationValidation(t *testing.T) {
	for _, raw := range []string{"https://service.example/api", "http://127.0.0.1:8091", "http://[::1]:8091", "http://localhost", "http://corporate:8091", "http://ollama:11434", "http://host.docker.internal:11434"} {
		if _, err := ValidateBaseURL(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	for _, raw := range []string{"", "not-a-url", "http://service.example", "ftp://localhost", "https://user:password@service.example", "https://service.example?key=value", "https://service.example?", "https://service.example#", "https://service.example#fragment", " https://service.example", "https://:80"} {
		if _, err := ValidateBaseURL(raw); err == nil {
			t.Fatal("unsafe destination accepted", raw)
		}
	}
}

func TestRedirectNeverResendsCredentialsOrBody(t *testing.T) {
	var forwarded atomic.Int32
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		forwarded.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer sink.Close()
	for _, status := range []int{301, 302, 303, 307, 308} {
		entry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, sink.URL, status)
		}))
		req, _ := http.NewRequest(http.MethodPost, entry.URL, strings.NewReader("synthetic private context"))
		req.Header.Set("Authorization", "Bearer synthetic-test-key")
		resp, err := Client(time.Second).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		entry.Close()
		if resp.StatusCode != status || forwarded.Load() != 0 {
			t.Fatal("redirect followed", status, forwarded.Load())
		}
	}
}

func TestLocalNamesCannotResolveToPublicDestinations(t *testing.T) {
	for _, tc := range []struct {
		host, ip string
		allowed  bool
	}{
		{"localhost", "127.0.0.1", true}, {"localhost", "203.0.113.17", false}, {"localhost", "10.0.0.1", false},
		{"ollama", "172.17.0.2", true}, {"corporate", "192.168.1.2", true}, {"host.docker.internal", "10.0.0.2", true},
		{"ollama", "203.0.113.17", false}, {"ollama", "169.254.169.254", false},
	} {
		if localAddressAllowed(tc.host, net.ParseIP(tc.ip)) != tc.allowed {
			t.Fatal(tc)
		}
	}
	transport := Client(time.Second).Transport.(*http.Transport)
	proxy, err := transport.Proxy(&http.Request{URL: &url.URL{Scheme: "http", Host: "corporate:8091"}})
	if err != nil || proxy != nil {
		t.Fatal("private service used environment proxy", proxy, err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer server.Close()
	resp, err := Client(time.Second).Get(strings.Replace(server.URL, "127.0.0.1", "localhost", 1))
	if err != nil {
		t.Fatal("localhost lost IPv4/IPv6 fallback", err)
	}
	resp.Body.Close()
}
