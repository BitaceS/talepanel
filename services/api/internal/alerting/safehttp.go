package alerting

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Alert webhooks are user-supplied URLs fetched by the API process, which sits
// next to Postgres, Redis, the daemons and (in cloud deployments) a metadata
// endpoint. Two layers stop them from being used to reach those:
//
//  1. ValidateWebhookURL rejects anything but https URLs with a plain hostname
//     when a rule is saved (Discord channels: only Discord's webhook path).
//  2. NewSafeHTTPClient refuses to connect to any non-public address. The check
//     runs in the dialer's Control hook, i.e. on the IP actually being dialled
//     after DNS resolution, so a hostname that resolves (or later re-resolves,
//     DNS rebinding) to an internal address is still blocked. Redirects are not
//     followed at all.

var ErrInvalidWebhookURL = errors.New("invalid webhook url")

// discordWebhookHosts are the hosts Discord serves channel webhooks from.
var discordWebhookHosts = map[string]bool{
	"discord.com":        true,
	"discordapp.com":     true,
	"ptb.discord.com":    true,
	"canary.discord.com": true,
}

// ValidateWebhookURL checks a rule's webhook URL against the channels it will
// be used for. An empty URL is allowed only when no channel needs one.
func ValidateWebhookURL(raw string, channels []string) error {
	needsWebhook, needsDiscord := false, false
	for _, ch := range channels {
		switch ch {
		case "webhook":
			needsWebhook = true
		case "discord":
			needsDiscord = true
		}
	}
	if raw == "" {
		if needsWebhook || needsDiscord {
			return fmt.Errorf("%w: webhook_url is required for webhook/discord channels", ErrInvalidWebhookURL)
		}
		return nil
	}
	if len(raw) > 2048 {
		return fmt.Errorf("%w: too long", ErrInvalidWebhookURL)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: unparseable", ErrInvalidWebhookURL)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("%w: only https URLs are allowed", ErrInvalidWebhookURL)
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in URL are not allowed", ErrInvalidWebhookURL)
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrInvalidWebhookURL)
	}
	if p := u.Port(); p != "" && p != "443" {
		return fmt.Errorf("%w: only the default https port is allowed", ErrInvalidWebhookURL)
	}
	// IP literals are only allowed when public; hostnames are checked again at
	// dial time against what they resolve to.
	if addr, err := netip.ParseAddr(host); err == nil {
		if !IsPublicAddr(addr) {
			return fmt.Errorf("%w: address is not public", ErrInvalidWebhookURL)
		}
	} else if host == "localhost" || strings.HasSuffix(host, ".localhost") || !strings.Contains(host, ".") {
		return fmt.Errorf("%w: host is not public", ErrInvalidWebhookURL)
	}
	if needsDiscord {
		if !discordWebhookHosts[host] || !strings.HasPrefix(u.EscapedPath(), "/api/webhooks/") {
			return fmt.Errorf("%w: discord channel requires a https://discord.com/api/webhooks/... URL", ErrInvalidWebhookURL)
		}
	}
	return nil
}

// cgnat is the RFC 6598 shared address space, not covered by netip.IsPrivate.
var (
	cgnat     = netip.MustParsePrefix("100.64.0.0/10")
	nat64     = netip.MustParsePrefix("64:ff9b::/96")
	thisNet   = netip.MustParsePrefix("0.0.0.0/8")
	benchmark = netip.MustParsePrefix("198.18.0.0/15")
	reserved  = netip.MustParsePrefix("240.0.0.0/4")
	ietf      = netip.MustParsePrefix("192.0.0.0/24")
)

// IsPublicAddr reports whether addr is a globally routable unicast address.
// IPv4-mapped IPv6 addresses are judged by their IPv4 form.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() ||
		addr.IsLoopback() ||
		addr.IsPrivate() || // 10/8, 172.16/12, 192.168/16, fc00::/7 (ULA)
		addr.IsLinkLocalUnicast() || // 169.254/16 (incl. cloud metadata), fe80::/10
		addr.IsLinkLocalMulticast() ||
		addr.IsInterfaceLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified() {
		return false
	}
	for _, p := range []netip.Prefix{cgnat, nat64, thisNet, benchmark, reserved, ietf} {
		if p.Contains(addr) {
			return false
		}
	}
	if addr.Is4() && addr == netip.AddrFrom4([4]byte{255, 255, 255, 255}) {
		return false
	}
	return true
}

// safeDialControl runs after DNS resolution with the concrete address being
// dialled and aborts the connection when it is not public.
func safeDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("webhook dial: %w", err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("webhook dial: unparseable address %q", host)
	}
	if !IsPublicAddr(addr) {
		return fmt.Errorf("webhook dial: refusing non-public address %s", addr)
	}
	return nil
}

// NewSafeHTTPClient returns an http.Client for user-supplied URLs: it never
// connects to non-public addresses, ignores proxy environment variables (a
// proxy would make the dialled address meaningless) and does not follow
// redirects.
func NewSafeHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   5 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   safeDialControl,
	}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		},
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: time.Second,
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
