package provisioner

import (
	"net/url"

	"golang.org/x/xerrors"
)

// ValidateExternalURL validates that value is a URL a browser can parse and
// navigate to. External app URLs are handed to the browser as-is, so they
// must carry a scheme and a host; without them the browser either refuses
// the link or resolves it against the dashboard's own origin.
func ValidateExternalURL(value string) error {
	u, err := url.Parse(value)
	if err != nil {
		return xerrors.Errorf("parse URL: %w", err)
	}

	if u.Scheme == "" {
		return xerrors.New(`must include a scheme, for example "https://"`)
	}

	if u.Host == "" || u.Hostname() == "" {
		return xerrors.Errorf("%q URLs must include a host", u.Scheme)
	}

	return nil
}
