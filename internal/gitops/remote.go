package gitops

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

// CanonicalRemoteIdentity normalizes a Git remote for identity comparison.
// Network remotes use lowercase host plus a case-preserving repository path.
func CanonicalRemoteIdentity(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\r\n\t ") {
		return "", fmt.Errorf("invalid Git remote %q", value)
	}

	if !strings.Contains(value, "://") {
		if at := strings.IndexByte(value, '@'); at > 0 {
			if colon := strings.IndexByte(value[at+1:], ':'); colon >= 0 {
				colon += at + 1
				return canonicalNetworkRemote(value[at+1:colon], value[colon+1:])
			}
		}
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parsing Git remote: %w", err)
	}
	switch parsed.Scheme {
	case "http", "https", "ssh", "git":
		host := parsed.Hostname()
		if port := parsed.Port(); port != "" {
			host += ":" + port
		}
		return canonicalNetworkRemote(host, parsed.Path)
	case "file":
		if parsed.Path == "" {
			return "", fmt.Errorf("file Git remote has no path")
		}
		path, err := filepath.Abs(filepath.Clean(parsed.Path))
		if err != nil {
			return "", fmt.Errorf("resolving file Git remote: %w", err)
		}
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = resolved
		}
		return "file:" + path, nil
	default:
		return "", fmt.Errorf("unsupported Git remote scheme %q", parsed.Scheme)
	}
}

func canonicalNetworkRemote(host, repositoryPath string) (string, error) {
	host = strings.ToLower(strings.TrimSpace(host))
	repositoryPath = strings.Trim(strings.TrimSpace(repositoryPath), "/")
	repositoryPath = strings.TrimSuffix(repositoryPath, ".git")
	if host == "" || repositoryPath == "" {
		return "", fmt.Errorf("git remote must include host and repository path")
	}
	return host + "/" + repositoryPath, nil
}
