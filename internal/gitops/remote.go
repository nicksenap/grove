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

// ResolveCommit resolves a Recipe ref to an immutable commit SHA. Unqualified
// refs prefer the fetched origin ref over a potentially stale local branch.
func ResolveCommit(repoPath, ref string) (string, error) {
	if isHexObjectID(ref) {
		return resolveObjectID(repoPath, ref)
	}
	candidates, err := exactRefCandidates(repoPath, ref)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidates {
		sha, err := runGit(repoPath, "rev-parse", "--verify", "--end-of-options", candidate+"^{commit}")
		if err == nil && sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("git ref %q does not resolve to a fetched branch, exact tag, or commit", ref)
}

// LocalBranchCommit resolves an already-provisioned local branch. Recipe ref
// resolution must not call this because local branches are not fetched inputs.
func LocalBranchCommit(repoPath, branch string) (string, error) {
	if _, err := runGit(repoPath, "check-ref-format", "--branch", branch); err != nil {
		return "", fmt.Errorf("invalid local branch %q", branch)
	}
	sha, err := runGit(repoPath, "rev-parse", "--verify", "--end-of-options", "refs/heads/"+branch+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolving local branch %q: %w", branch, err)
	}
	return sha, nil
}

func resolveObjectID(repoPath, objectID string) (string, error) {
	matches, err := runGit(repoPath, "rev-parse", "--disambiguate="+strings.ToLower(objectID))
	if err != nil {
		return "", fmt.Errorf("resolving commit object %q: %w", objectID, err)
	}
	var objects []string
	for _, match := range strings.Split(matches, "\n") {
		if match = strings.TrimSpace(match); match != "" {
			objects = append(objects, match)
		}
	}
	if len(objects) != 1 {
		return "", fmt.Errorf("commit object %q is missing or ambiguous", objectID)
	}
	sha, err := runGit(repoPath, "rev-parse", "--verify", "--end-of-options", objects[0]+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("object %q is not a commit", objectID)
	}
	return sha, nil
}

func exactRefCandidates(repoPath, ref string) ([]string, error) {
	if ref == "" || strings.HasPrefix(ref, "-") || strings.ContainsAny(ref, "\x00\r\n\t ") {
		return nil, fmt.Errorf("invalid Git ref %q", ref)
	}
	if strings.HasPrefix(ref, "origin/") {
		ref = "refs/remotes/origin/" + strings.TrimPrefix(ref, "origin/")
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		ref = "refs/remotes/origin/" + strings.TrimPrefix(ref, "refs/heads/")
	}
	if strings.HasPrefix(ref, "refs/") {
		if !strings.HasPrefix(ref, "refs/tags/") && !strings.HasPrefix(ref, "refs/remotes/origin/") {
			return nil, fmt.Errorf("unsupported full Git ref %q", ref)
		}
		if _, err := runGit(repoPath, "check-ref-format", ref); err != nil {
			return nil, fmt.Errorf("invalid Git ref %q", ref)
		}
		return []string{ref}, nil
	}
	if _, err := runGit(repoPath, "check-ref-format", "refs/heads/"+ref); err != nil {
		return nil, fmt.Errorf("invalid Git ref %q", ref)
	}
	return []string{"refs/remotes/origin/" + ref, "refs/tags/" + ref}, nil
}

func isHexObjectID(value string) bool {
	if len(value) < 7 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F' {
			continue
		}
		return false
	}
	return true
}
