package daemon

import (
	"errors"
	"mime"
	"net"
	"net/netip"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// ErrInvalidNodeAddress is returned for a node fqdn/port that cannot be used
// to build a daemon URL.
var ErrInvalidNodeAddress = errors.New("invalid node address")

// ValidateNodeHost checks that host is a bare hostname or IP literal, so it
// can be placed into a URL without changing its meaning (no scheme, path,
// query, fragment, userinfo, port or whitespace). Private addresses and
// single-label names are fine: nodes legitimately live on private networks or
// behind Docker service names such as "daemon". IPv6 may be given with or
// without brackets.
func ValidateNodeHost(host string) error {
	if host == "" || len(host) > 253 {
		return ErrInvalidNodeAddress
	}
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if addr.Zone() != "" {
			return ErrInvalidNodeAddress
		}
		return nil
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ErrInvalidNodeAddress
		}
		for _, r := range label {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			default:
				return ErrInvalidNodeAddress
			}
		}
	}
	return nil
}

// ValidateNodePort checks a daemon port.
func ValidateNodePort(port int) error {
	if port < 1 || port > 65535 {
		return ErrInvalidNodeAddress
	}
	return nil
}

// BaseURL builds "http://host:port" for a node after validating both parts.
func BaseURL(host string, port int) (string, error) {
	if err := ValidateNodeHost(host); err != nil {
		return "", err
	}
	if err := ValidateNodePort(port); err != nil {
		return "", err
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(port))}
	return u.String(), nil
}

// endpoint joins the client's base URL with an already-built path and
// properly encoded query parameters.
func (c *Client) endpoint(p string, query url.Values) string {
	u := c.baseURL + p
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	return u
}

// filenameFromDisposition extracts a safe base filename from a
// Content-Disposition header value, falling back when it is missing or
// malformed.
func filenameFromDisposition(cd, fallback string) string {
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return fallback
	}
	name := SanitizeFilename(params["filename"])
	if name == "" {
		return fallback
	}
	return name
}

// SanitizeFilename reduces a name to a single path element without control
// characters, suitable for a download filename.
func SanitizeFilename(name string) string {
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, name)
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == ".." {
		return ""
	}
	return name
}

// AttachmentDisposition builds a Content-Disposition header for a download
// with mime.FormatMediaType, which quotes/escapes the name (or RFC 2231-encodes
// it when non-ASCII) so it cannot inject header parameters.
func AttachmentDisposition(name string) string {
	name = SanitizeFilename(name)
	if name == "" {
		return "attachment"
	}
	if v := mime.FormatMediaType("attachment", map[string]string{"filename": name}); v != "" {
		return v
	}
	return "attachment"
}
