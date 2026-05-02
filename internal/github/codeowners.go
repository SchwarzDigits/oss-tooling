package github

import (
	"context"
	"errors"
	"net/http"
	"strings"

	gogithub "github.com/google/go-github/v66/github"
)

// codeownersSearchPaths is the precedence order GitHub itself uses (root,
// .github, docs). The first existing file wins.
var codeownersSearchPaths = []string{
	"CODEOWNERS",
	".github/CODEOWNERS",
	"docs/CODEOWNERS",
}

// GetCodeOwners reads the first CODEOWNERS file that exists at the standard
// paths and returns the deduplicated list of @-prefixed owner tokens. When
// no CODEOWNERS file exists, it returns (nil, nil) — that's not an error.
func GetCodeOwners(ctx context.Context, c *gogithub.Client, owner, repo, ref string) ([]string, error) {
	for _, path := range codeownersSearchPaths {
		body, found, err := fetchTextFile(ctx, c, owner, repo, path, ref)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		return parseCodeOwners(body), nil
	}
	return nil, nil
}

// fetchTextFile returns the decoded content of the file at path. When the
// file does not exist (404), it returns ("", false, nil). All other errors
// (network, decoding, permission) propagate.
func fetchTextFile(ctx context.Context, c *gogithub.Client, owner, repo, path, ref string) (string, bool, error) {
	opt := &gogithub.RepositoryContentGetOptions{Ref: ref}
	content, err := WithRetry(ctx, func(ctx context.Context) (*gogithub.RepositoryContent, error) {
		f, _, _, err := c.Repositories.GetContents(ctx, owner, repo, path, opt)
		return f, err
	})
	if err != nil {
		var ghErr *gogithub.ErrorResponse
		if errors.As(err, &ghErr) && ghErr.Response != nil && ghErr.Response.StatusCode == http.StatusNotFound {
			return "", false, nil
		}
		return "", false, err
	}
	if content == nil {
		return "", false, nil
	}
	body, err := content.GetContent()
	if err != nil {
		return "", false, err
	}
	return body, true, nil
}

// parseCodeOwners extracts every @-prefixed token from a CODEOWNERS file.
// Lines beginning with '#' (after optional leading whitespace) are comments
// and are skipped. Blank lines are ignored. Multiple owners on a single line
// each contribute one entry. Order is first-seen.
func parseCodeOwners(body string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		for _, tok := range strings.Fields(line) {
			if !strings.HasPrefix(tok, "@") {
				continue
			}
			if _, dup := seen[tok]; dup {
				continue
			}
			seen[tok] = struct{}{}
			out = append(out, tok)
		}
	}
	return out
}
