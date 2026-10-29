package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/yosida95/uritemplate/v3"

	"github.com/cornelius-99/bitbucket-mcp/internal/bitbucket"
)

// Everything that only reads from Bitbucket is both a resource template and a
// read-only tool backed by the same function. Many clients only let the model
// call tools, so the tools are what an agent uses; the resources let a user or
// client attach a pull request, its diff and so on as context.
//
// Resource URIs mirror Bitbucket's web URLs under the bitbucket:// scheme, and
// optional parameters go in the query string, for example
// bitbucket://projects/PRJ/repos/repo/pull-requests?state=MERGED&limit=10.
//
// Each read takes an args struct. For the tool it is the input schema. For the
// resource, path variables and query parameters are matched to its fields by
// their json names, and every field that is not a path variable becomes a
// query parameter of the template, documented by its jsonschema tag.

const (
	repoURI = "bitbucket://projects/{project_key}/repos/{repository_slug}"
	prURI   = repoURI + "/pull-requests/{pull_request_id}"

	mimeJSON = "application/json"
	mimeDiff = "text/x-diff"
)

// PageArgs are Bitbucket's standard paging parameters.
type PageArgs struct {
	Start int `json:"start,omitempty" jsonschema:"Index of the first item to return (from nextPageStart of the previous page)"`
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of items to return (Bitbucket default 25)"`
}

func (p PageArgs) start() *float32 { return optNum(p.Start) }
func (p PageArgs) limit() *float32 { return optNum(p.Limit) }

// RawArgs lets a caller ask for Bitbucket's unmodified response.
type RawArgs struct {
	Raw bool `json:"raw,omitempty" jsonschema:"Return Bitbucket's full, unmodified JSON instead of the compact summary"`
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optNum(n int) *float32 {
	if n == 0 {
		return nil
	}
	f := float32(n)
	return &f
}

func optBool(b *bool) *string {
	if b == nil {
		return nil
	}
	s := strconv.FormatBool(*b)
	return &s
}

type reads struct {
	c *bitbucket.Client
}

// register adds all reads to s, each as a tool and a resource template.
func (r *reads) register(s *mcp.Server) {
	addRead(s, "list_pull_requests", &mcp.ResourceTemplate{
		Name:        "pull_requests",
		Title:       "Pull requests in a repository",
		URITemplate: repoURI + "/pull-requests",
		Description: "A page (values, isLastPage, nextPageStart) of PR summaries without descriptions; get a single pull request for the full PR.",
		MIMEType:    mimeJSON,
	}, r.pullRequests)

	addRead(s, "list_my_pull_requests", &mcp.ResourceTemplate{
		Name:        "my_pull_requests",
		Title:       "My pull requests",
		URITemplate: "bitbucket://dashboard/pull-requests",
		Description: "Pull requests across all repositories that involve the authenticated user (the Bitbucket dashboard), as summaries without descriptions.",
		MIMEType:    mimeJSON,
	}, r.myPullRequests)

	addRead(s, "get_pull_request", &mcp.ResourceTemplate{
		Name:        "pull_request",
		Title:       "Pull request",
		URITemplate: prURI,
		Description: "A pull request, including its title, description, branches, reviewers, state and version.",
		MIMEType:    mimeJSON,
	}, r.pullRequest)

	addRead(s, "get_pull_request_diff", &mcp.ResourceTemplate{
		Name:        "pull_request_diff",
		Title:       "Pull request diff",
		URITemplate: prURI + "/diff",
		Description: "The unified diff of a pull request as plain text. Output over 1 MiB is truncated.",
		MIMEType:    mimeDiff,
	}, r.diff)

	addRead(s, "get_pull_request_changes", &mcp.ResourceTemplate{
		Name:        "pull_request_changes",
		Title:       "Pull request changes",
		URITemplate: prURI + "/changes",
		Description: "The files changed by a pull request.",
		MIMEType:    mimeJSON,
	}, r.changes)

	addRead(s, "get_pull_request_commits", &mcp.ResourceTemplate{
		Name:        "pull_request_commits",
		Title:       "Pull request commits",
		URITemplate: prURI + "/commits",
		Description: "The commits of a pull request.",
		MIMEType:    mimeJSON,
	}, r.commits)

	addRead(s, "get_pull_request_activities", &mcp.ResourceTemplate{
		Name:        "pull_request_activities",
		Title:       "Pull request activity",
		URITemplate: prURI + "/activities",
		Description: "The activity of a pull request, newest first: comments (with replies), approvals, reviews, rescopes, merges and declines.",
		MIMEType:    mimeJSON,
	}, r.activities)

	addRead(s, "get_pull_request_merge_status", &mcp.ResourceTemplate{
		Name:        "pull_request_merge_status",
		Title:       "Pull request merge status",
		URITemplate: prURI + "/merge-status",
		Description: "Whether a pull request can be merged, and any vetoes (e.g. missing approvals, failing builds, conflicts).",
		MIMEType:    mimeJSON,
	}, r.mergeStatus)
}

// addRead registers read as a read-only tool named tool and as the resource
// template t, whose URITemplate holds only the path. The fields of A that are
// not path variables are appended to the template as query parameters and
// described in its description. Each resource read decodes the request URI
// into an A.
func addRead[A any](s *mcp.Server, tool string, t *mcp.ResourceTemplate, read func(context.Context, A) (string, error)) {
	mcp.AddTool(s, &mcp.Tool{
		Name:        tool,
		Title:       t.Title,
		Description: t.Description,
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(true)},
	}, func(ctx context.Context, _ *mcp.CallToolRequest, a A) (*mcp.CallToolResult, any, error) {
		text, err := read(ctx, a)
		if err != nil {
			return nil, nil, err
		}
		return textResult(text), nil, nil
	})

	path := uritemplate.MustNew(t.URITemplate)
	fields := map[string][]int{} // json name to field index
	var query, docs []string
	for _, f := range reflect.VisibleFields(reflect.TypeFor[A]()) {
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous || name == "" {
			continue
		}
		fields[name] = f.Index
		if !slices.Contains(path.Varnames(), name) {
			query = append(query, name)
			docs = append(docs, name+": "+strings.TrimSuffix(f.Tag.Get("jsonschema"), ".")+".")
		}
	}
	for _, v := range path.Varnames() {
		if _, ok := fields[v]; !ok {
			panic(fmt.Sprintf("%s: no field for path variable %q", t.URITemplate, v))
		}
	}
	if len(query) > 0 {
		t.URITemplate += "{?" + strings.Join(query, ",") + "}"
		t.Description += " Query parameters, all optional: " + strings.Join(docs, " ")
	}

	s.AddResourceTemplate(t, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		var a A
		if err := decodeURI(req.Params.URI, path, fields, reflect.ValueOf(&a).Elem()); err != nil {
			return nil, err
		}
		text, err := read(ctx, a)
		if err != nil {
			return nil, err
		}
		// The SDK fills in the URI and the template's MIME type.
		return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{Text: text}}}, nil
	})
}

// decodeURI sets the fields of v from the path variables and query parameters
// of uri. An empty query value (name=) leaves its field unset, as RFC 6570
// expands an empty variable that way, and a bare boolean name (?raw) is true.
func decodeURI(uri string, path *uritemplate.Template, fields map[string][]int, v reflect.Value) error {
	p, q, _ := strings.Cut(uri, "?")
	vals := path.Match(p)
	if vals == nil {
		return mcp.ResourceNotFoundError(uri)
	}
	params := map[string]string{}
	for _, name := range path.Varnames() {
		s := vals.Get(name).String()
		if s == "" {
			return invalidParams(uri, "%s must not be empty", name)
		}
		params[name] = s
	}
	for kv := range strings.SplitSeq(q, "&") {
		if kv == "" {
			continue
		}
		k, s, hasValue := strings.Cut(kv, "=")
		name, err := url.QueryUnescape(k)
		if err != nil {
			return invalidParams(uri, "invalid query: %v", err)
		}
		index, ok := fields[name]
		switch {
		case !ok:
			return invalidParams(uri, "unknown parameter %q", name)
		case slices.Contains(path.Varnames(), name):
			return invalidParams(uri, "%s is part of the path, not a query parameter", name)
		}
		if _, dup := params[name]; dup {
			return invalidParams(uri, "%s is given more than once", name)
		}
		if !hasValue {
			if !isBool(v.FieldByIndex(index).Type()) {
				return invalidParams(uri, "%s needs a value", name)
			}
			s = "true"
		} else if s, err = url.QueryUnescape(s); err != nil {
			return invalidParams(uri, "invalid query: %v", err)
		}
		params[name] = s
	}
	for name, s := range params {
		if s == "" {
			continue
		}
		if err := setField(v.FieldByIndex(fields[name]), s); err != nil {
			return invalidParams(uri, "%s %v, got %q", name, err, s)
		}
	}
	return nil
}

func invalidParams(uri, format string, args ...any) error {
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: uri + ": " + fmt.Sprintf(format, args...)}
}

func isBool(t reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Kind() == reflect.Bool
}

func setField(v reflect.Value, s string) error {
	switch v.Kind() {
	case reflect.String:
		v.SetString(s)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return errors.New("must be an integer")
		}
		v.SetInt(n)
	case reflect.Bool:
		b, err := strconv.ParseBool(s)
		if err != nil {
			return errors.New("must be true or false")
		}
		v.SetBool(b)
	case reflect.Pointer:
		p := reflect.New(v.Type().Elem())
		if err := setField(p.Elem(), s); err != nil {
			return err
		}
		v.Set(p)
	default:
		panic("unsupported URI parameter type " + v.Type().String())
	}
	return nil
}

type ListPullRequestsArgs struct {
	RepoArgs
	State      string `json:"state,omitempty" jsonschema:"OPEN (default), DECLINED, MERGED or ALL"`
	Direction  string `json:"direction,omitempty" jsonschema:"INCOMING (default, PRs targeting this repo) or OUTGOING (PRs from this repo)"`
	At         string `json:"at,omitempty" jsonschema:"Only PRs to (INCOMING) or from (OUTGOING) this fully qualified ref, e.g. refs/heads/main"`
	Order      string `json:"order,omitempty" jsonschema:"OLDEST or NEWEST (default)"`
	FilterText string `json:"filter_text,omitempty" jsonschema:"Only PRs whose title or description contains this text"`
	Draft      *bool  `json:"draft,omitempty" jsonschema:"true for only drafts, false for only non-drafts; omit for both"`
	PageArgs
	RawArgs
}

func (r *reads) pullRequests(ctx context.Context, a ListPullRequestsArgs) (string, error) {
	body, err := readBody(r.c.GetPage(ctx, a.ProjectKey, a.RepositorySlug, &bitbucket.GetPageParams{
		State:      optStr(a.State),
		Direction:  optStr(a.Direction),
		At:         optStr(a.At),
		Order:      optStr(a.Order),
		FilterText: optStr(a.FilterText),
		Draft:      optBool(a.Draft),
		Start:      a.start(),
		Limit:      a.limit(),
	}))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, compactPage(toPullRequestSummary))
}

type ListMyPullRequestsArgs struct {
	Role              string `json:"role,omitempty" jsonschema:"Only PRs where the user has this role: AUTHOR, REVIEWER or PARTICIPANT"`
	State             string `json:"state,omitempty" jsonschema:"OPEN, DECLINED or MERGED; omit for any state"`
	ParticipantStatus string `json:"participant_status,omitempty" jsonschema:"Comma separated participant statuses: UNAPPROVED, NEEDS_WORK, APPROVED"`
	Order             string `json:"order,omitempty" jsonschema:"OLDEST, NEWEST (default), DRAFT_STATUS, PARTICIPANT_STATUS or CLOSED_DATE"`
	PageArgs
	RawArgs
}

func (r *reads) myPullRequests(ctx context.Context, a ListMyPullRequestsArgs) (string, error) {
	body, err := readBody(r.c.GetPullRequests1(ctx, &bitbucket.GetPullRequests1Params{
		Role:              optStr(a.Role),
		State:             optStr(a.State),
		ParticipantStatus: optStr(a.ParticipantStatus),
		Order:             optStr(a.Order),
		Start:             a.start(),
		Limit:             a.limit(),
	}))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, compactPage(toPullRequestSummary))
}

type GetPullRequestArgs struct {
	PRArgs
	RawArgs
}

func (r *reads) pullRequest(ctx context.Context, a GetPullRequestArgs) (string, error) {
	body, err := readBody(r.c.Get3(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, toPullRequest)
}

type GetDiffArgs struct {
	PRArgs
	ContextLines *int   `json:"context_lines,omitempty" jsonschema:"Lines of context around each change (Bitbucket default 10)"`
	Whitespace   string `json:"whitespace,omitempty" jsonschema:"Set to ignore-all to ignore whitespace changes"`
}

func (r *reads) diff(ctx context.Context, a GetDiffArgs) (string, error) {
	params := &bitbucket.StreamRawDiff2Params{Whitespace: optStr(a.Whitespace)}
	if a.ContextLines != nil {
		params.ContextLines = optStr(strconv.Itoa(*a.ContextLines))
	}
	body, err := readBody(r.c.StreamRawDiff2(ctx, a.ProjectKey, a.RepositorySlug, a.id(), params, acceptText))
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "(empty diff)", nil
	}
	return string(body), nil
}

type GetChangesArgs struct {
	PRArgs
	ChangeScope string `json:"change_scope,omitempty" jsonschema:"ALL (default), UNREVIEWED (changes since the user's last review) or RANGE (use since_id and until_id)"`
	SinceID     string `json:"since_id,omitempty" jsonschema:"With change_scope RANGE: the commit to diff from"`
	UntilID     string `json:"until_id,omitempty" jsonschema:"With change_scope RANGE: the commit to diff to"`
	PageArgs
	RawArgs
}

func (r *reads) changes(ctx context.Context, a GetChangesArgs) (string, error) {
	body, err := readBody(r.c.StreamChanges1(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.StreamChanges1Params{
		ChangeScope: optStr(a.ChangeScope),
		SinceId:     optStr(a.SinceID),
		UntilId:     optStr(a.UntilID),
		Start:       a.start(),
		Limit:       a.limit(),
	}))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, compactPage(toChange))
}

type PagedPRArgs struct {
	PRArgs
	PageArgs
	RawArgs
}

func (r *reads) commits(ctx context.Context, a PagedPRArgs) (string, error) {
	body, err := readBody(r.c.GetCommits1(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.GetCommits1Params{
		Start: a.start(),
		Limit: a.limit(),
	}))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, compactPage(toCommit))
}

func (r *reads) activities(ctx context.Context, a PagedPRArgs) (string, error) {
	body, err := readBody(r.c.GetActivities(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.GetActivitiesParams{
		Start: a.start(),
		Limit: a.limit(),
	}))
	if err != nil {
		return "", err
	}
	return compactJSON(body, a.Raw, compactPage(toActivity))
}

// mergeStatus accepts raw like every other JSON read, but Bitbucket's merge
// status is already compact, so it is always returned unmodified.
func (r *reads) mergeStatus(ctx context.Context, a GetPullRequestArgs) (string, error) {
	body, err := readBody(r.c.CanMerge(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return "", err
	}
	return string(body), nil
}

// acceptText asks for a plain-text response, used for raw diffs.
func acceptText(_ context.Context, req *http.Request) error {
	req.Header.Set("Accept", "text/plain")
	return nil
}
