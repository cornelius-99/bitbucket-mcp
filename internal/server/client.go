package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/cornelius-99/bitbucket-mcp/internal/bitbucket"
)

// maxResponseBytes caps how much of a Bitbucket response is returned to the
// model. Large diffs are truncated rather than flooding the context window.
const maxResponseBytes = 1 << 20

// NewClient returns a generated Bitbucket client that authenticates every
// request with the given HTTP access token.
//
// baseURL is the Bitbucket base URL as seen in the browser, for example
// https://bitbucket.example.com or https://example.com/bitbucket.
func NewClient(baseURL, token, userAgent string) (*bitbucket.Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("bitbucket base URL is empty")
	}
	if token == "" {
		return nil, errors.New("bitbucket token is empty")
	}
	return bitbucket.NewClient(baseURL+"/rest",
		bitbucket.WithHTTPClient(&http.Client{Timeout: 60 * time.Second}),
		bitbucket.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("User-Agent", userAgent)
			if req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", "application/json")
			}
			return nil
		}),
	)
}

// APIError is returned when Bitbucket answers with a non-2xx status.
type APIError struct {
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("bitbucket returned %d: %s", e.StatusCode, e.Body)
}

// readBody consumes resp and returns its body, or an *APIError for non-2xx
// responses. err is the error returned alongside resp by the client call.
func readBody(resp *http.Response, err error) ([]byte, error) {
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading bitbucket response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	if len(body) > maxResponseBytes {
		body = append(body[:maxResponseBytes], []byte("\n... [truncated]")...)
	}
	return body, nil
}

// decodeBody is readBody followed by a JSON decode into v.
func decodeBody(resp *http.Response, err error, v any) error {
	body, err := readBody(resp, err)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decoding bitbucket response: %w", err)
	}
	return nil
}
