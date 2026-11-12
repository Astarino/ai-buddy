package outbound

import (
	"context"
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
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = func(r *http.Request) (*url.URL, error) {
		if Local(r.URL) {
			return nil, nil
		}
		return http.ProxyFromEnvironment(r)
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		local := Local(&url.URL{Host: host})
		if !local {
			return dialer.DialContext(ctx, network, address)
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		var last error
		for _, ip := range ips {
			if localAddressAllowed(host, ip.IP) {
				connection, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
				if err == nil {
					return connection, nil
				}
				last = err
			}
		}
		if last != nil {
			return nil, last
		}
		return nil, fmt.Errorf("local service resolved outside the approved network")
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			// A redirect must never move a prompt, action or credential to another destination.
			return http.ErrUseLastResponse
		},
	}
}

func localAddressAllowed(host string, ip net.IP) bool {
	if ip.IsLoopback() {
		return true
	}
	return (host == "ollama" || host == "corporate" || host == "host.docker.internal") && ip.IsPrivate()
}
