package outbound

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func Local(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "host.docker.internal" || host == "ollama" || host == "corporate" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ValidateBaseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") || strings.TrimSpace(raw) != raw {
		return nil, fmt.Errorf("invalid service URL")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && Local(u)) {
		return nil, fmt.Errorf("service URL requires HTTPS or local HTTP")
	}
	return u, nil
}

func Client(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// A redirect must never move a prompt, action or credential to another destination.
			return http.ErrUseLastResponse
		},
	}
}
