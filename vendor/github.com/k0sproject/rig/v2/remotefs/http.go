package remotefs

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrHTTPStatusNotSupported is returned by HTTPStatusInsecure when the FS
// implementation does not support HTTP status checks. Callers can detect this
// with errors.Is.
var ErrHTTPStatusNotSupported = errors.New("http status check not supported by this filesystem type")

var (
	errURLInvalidCharacter    = errors.New("url contains invalid character")
	errURLContainsCredentials = errors.New("url must not contain credentials")
	errURLInvalidScheme       = errors.New("url scheme must be http or https")
	errURLMissingHost         = errors.New("url must contain a host")
)

// httpStatusProvider is implemented by FS types that support insecure HTTP status checks.
type httpStatusProvider interface {
	httpStatusInsecure(ctx context.Context, url string) (int, error)
}

func validateHTTPURL(rawURL string) error {
	for _, c := range rawURL {
		if c < 0x20 || c == 0x7f {
			return fmt.Errorf("%w: %q", errURLInvalidCharacter, c)
		}
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "http" && scheme != "https" {
		return fmt.Errorf("%w: %q", errURLInvalidScheme, u.Scheme)
	}
	if u.Host == "" {
		return errURLMissingHost
	}
	if u.User != nil {
		return errURLContainsCredentials
	}
	return nil
}

// HTTPStatusInsecure checks whether url is reachable and returns the HTTP status
// code, skipping TLS certificate verification. On Windows with PowerShell 5.x,
// TLS certificate verification is not skipped and requests will fail for
// self-signed certificates.
func HTTPStatusInsecure(ctx context.Context, fs FS, url string) (int, error) {
	if err := validateHTTPURL(url); err != nil {
		return 0, fmt.Errorf("HTTPStatusInsecure: %w", err)
	}
	p, ok := fs.(httpStatusProvider)
	if !ok {
		return 0, ErrHTTPStatusNotSupported
	}
	return p.httpStatusInsecure(ctx, url)
}
