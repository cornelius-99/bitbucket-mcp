package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type recorded struct {
	Method string
	Path   string
	Query  string
	Auth   string
	Accept string
	Body   map[string]any
}

// fakeBitbucket serves canned responses keyed by "METHOD /path" and records
// every request it receives.
type fakeBitbucket struct {
	t         *testing.T
	responses map[string]string
	requests  []recorded
}

func (f *fakeBitbucket) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := recorded{
		Method: r.Method,
		Path:   r.URL.Path,
		Query:  r.URL.RawQuery,
		Auth:   r.Header.Get("Authorization"),
		Accept: r.Header.Get("Accept"),
	}
	if b, _ := io.ReadAll(r.Body); len(b) > 0 {
		if err := json.Unmarshal(b, &rec.Body); err != nil {
			f.t.Errorf("request body is not JSON: %s", b)
		}
	}
	f.requests = append(f.requests, rec)

	resp, ok := f.responses[r.Method+" "+r.URL.Path]
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"errors":[{"message":"not found"}]}`)
		return
	}
	io.WriteString(w, resp)
}

func (f *fakeBitbucket) last() recorded {
	f.t.Helper()
	if len(f.requests) == 0 {
		f.t.Fatal("no request recorded")
	}
	return f.requests[len(f.requests)-1]
}

func setup(t *testing.T, responses map[string]string, opts Options) (*fakeBitbucket, *mcp.ClientSession) {
	t.Helper()
	fake := &fakeBitbucket{t: t, responses: responses}
	ts := httptest.NewServer(fake)
	t.Cleanup(ts.Close)

	c, err := NewClient(ts.URL+"/", "secret-token", "test")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := New(c, opts).Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return fake, cs
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (string, bool) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

const prPath = "/rest/api/latest/projects/PRJ/repos/repo/pull-requests/7"

func prArgs(extra map[string]any) map[string]any {
	m := map[string]any{"project_key": "PRJ", "repository_slug": "repo", "pull_request_id": 7}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestListTools(t *testing.T) {
	_, cs := setup(t, nil, Options{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	for _, want := range []string{"list_pull_requests", "get_pull_request", "merge_pull_request", "add_pull_request_comment"} {
		if !slices.Contains(names, want) {
			t.Errorf("tool %q missing from %v", want, names)
		}
	}
}

func TestReadOnlyHidesWriteTools(t *testing.T) {
	_, cs := setup(t, nil, Options{ReadOnly: true})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("read-only server exposes write tool %q", tool.Name)
		}
	}
	if len(res.Tools) == 0 {
		t.Error("no tools registered")
	}
}

func TestListPullRequests(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"GET /rest/api/latest/projects/PRJ/repos/repo/pull-requests": `{"values":[{"id":7}],"isLastPage":true}`,
	}, Options{})

	out, isErr := call(t, cs, "list_pull_requests", map[string]any{
		"project_key": "PRJ", "repository_slug": "repo", "state": "MERGED", "limit": 5,
	})
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	if !strings.Contains(out, `"id":7`) {
		t.Errorf("output = %s", out)
	}
	req := fake.last()
	if req.Auth != "Bearer secret-token" {
		t.Errorf("Authorization = %q", req.Auth)
	}
	if !strings.Contains(req.Query, "state=MERGED") || !strings.Contains(req.Query, "limit=5") {
		t.Errorf("query = %q", req.Query)
	}
}

func TestGetPullRequestDiffAsksForText(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"GET " + prPath + ".diff": "diff --git a/x b/x\n",
	}, Options{})

	out, isErr := call(t, cs, "get_pull_request_diff", prArgs(nil))
	if isErr || !strings.HasPrefix(out, "diff --git") {
		t.Fatalf("output = %q (error %v)", out, isErr)
	}
	if got := fake.last().Accept; got != "text/plain" {
		t.Errorf("Accept = %q", got)
	}
}

func TestAPIErrorIsToolError(t *testing.T) {
	_, cs := setup(t, nil, Options{})
	out, isErr := call(t, cs, "get_pull_request", prArgs(nil))
	if !isErr {
		t.Fatalf("expected tool error, got %s", out)
	}
	if !strings.Contains(out, "404") || !strings.Contains(out, "not found") {
		t.Errorf("error = %s", out)
	}
}

func TestCreatePullRequest(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"POST /rest/api/latest/projects/PRJ/repos/repo/pull-requests": `{"id":8}`,
	}, Options{})

	_, isErr := call(t, cs, "create_pull_request", map[string]any{
		"project_key": "PRJ", "repository_slug": "repo",
		"title": "Add x", "from_branch": "feature/x", "to_branch": "main",
		"reviewers": []string{"alice"},
	})
	if isErr {
		t.Fatal("unexpected error")
	}
	body := fake.last().Body
	if body["title"] != "Add x" {
		t.Errorf("title = %v", body["title"])
	}
	if id := body["fromRef"].(map[string]any)["id"]; id != "refs/heads/feature/x" {
		t.Errorf("fromRef.id = %v", id)
	}
	if id := body["toRef"].(map[string]any)["id"]; id != "refs/heads/main" {
		t.Errorf("toRef.id = %v", id)
	}
	rev := body["reviewers"].([]any)[0].(map[string]any)["user"].(map[string]any)["name"]
	if rev != "alice" {
		t.Errorf("reviewer = %v", rev)
	}
}

func TestUpdatePullRequestKeepsUnspecifiedFields(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"GET " + prPath: `{"id":7,"version":3,"title":"Old","description":"Desc","draft":false,
			"toRef":{"id":"refs/heads/main"},
			"reviewers":[{"user":{"name":"bob","displayName":"Bob"},"status":"APPROVED"}]}`,
		"PUT " + prPath: `{"id":7,"version":4}`,
	}, Options{})

	_, isErr := call(t, cs, "update_pull_request", prArgs(map[string]any{"title": "New"}))
	if isErr {
		t.Fatal("unexpected error")
	}
	body := fake.last().Body
	if body["title"] != "New" || body["description"] != "Desc" || body["version"] != float64(3) {
		t.Errorf("body = %v", body)
	}
	rev := body["reviewers"].([]any)
	if len(rev) != 1 || rev[0].(map[string]any)["user"].(map[string]any)["name"] != "bob" {
		t.Errorf("reviewers = %v", rev)
	}
}

func TestMergeFetchesVersion(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"GET " + prPath:             `{"id":7,"version":12}`,
		"POST " + prPath + "/merge": `{"id":7,"state":"MERGED"}`,
	}, Options{})

	out, isErr := call(t, cs, "merge_pull_request", prArgs(map[string]any{"strategy_id": "squash"}))
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	req := fake.last()
	if req.Path != prPath+"/merge" {
		t.Fatalf("path = %s", req.Path)
	}
	if req.Body["version"] != float64(12) || req.Body["strategyId"] != "squash" {
		t.Errorf("body = %v", req.Body)
	}
}

func TestAddInlineComment(t *testing.T) {
	fake, cs := setup(t, map[string]string{
		"POST " + prPath + "/comments": `{"id":99}`,
	}, Options{})

	_, isErr := call(t, cs, "add_pull_request_comment", prArgs(map[string]any{
		"text": "nit", "path": "src/a.go", "line": 10, "line_type": "REMOVED",
	}))
	if isErr {
		t.Fatal("unexpected error")
	}
	anchor := fake.last().Body["anchor"].(map[string]any)
	if anchor["path"] != "src/a.go" || anchor["line"] != float64(10) || anchor["fileType"] != "FROM" {
		t.Errorf("anchor = %v", anchor)
	}
}
