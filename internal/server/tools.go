package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cornelius-99/bitbucket-mcp/internal/bitbucket"
)

// RepoArgs identifies a repository.
type RepoArgs struct {
	ProjectKey     string `json:"project_key" jsonschema:"Bitbucket project key, e.g. PRJ. For a personal repository use ~username."`
	RepositorySlug string `json:"repository_slug" jsonschema:"Repository slug, e.g. my-repo"`
}

// PRArgs identifies a pull request.
type PRArgs struct {
	RepoArgs
	PullRequestID int64 `json:"pull_request_id" jsonschema:"Numeric pull request ID"`
}

func (a PRArgs) id() string { return strconv.FormatInt(a.PullRequestID, 10) }

// PageArgs are Bitbucket's standard paging parameters.
type PageArgs struct {
	Start int `json:"start,omitempty" jsonschema:"Index of the first item to return (from nextPageStart of the previous page)"`
	Limit int `json:"limit,omitempty" jsonschema:"Maximum number of items to return (Bitbucket default 25)"`
}

func (p PageArgs) start() *float32 { return optNum(p.Start) }
func (p PageArgs) limit() *float32 { return optNum(p.Limit) }

func optNum(n int) *float32 {
	if n == 0 {
		return nil
	}
	f := float32(n)
	return &f
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optBool(b *bool) *string {
	if b == nil {
		return nil
	}
	s := strconv.FormatBool(*b)
	return &s
}

// jsonResult wraps a raw Bitbucket JSON body as tool output.
func jsonResult(body []byte) *mcp.CallToolResult {
	return textResult(string(body))
}

func textResult(s string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
}

func jsonReader(v any) (*bytes.Reader, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(b), nil
}

const jsonContentType = "application/json"

type tools struct {
	c *bitbucket.Client
}

// currentVersion fetches the pull request and returns its version, used when
// the caller does not pass one explicitly.
func (t *tools) currentVersion(ctx context.Context, a PRArgs) (int32, error) {
	var pr struct {
		Version int32 `json:"version"`
	}
	resp, err := t.c.Get3(ctx, a.ProjectKey, a.RepositorySlug, a.id())
	if err := decodeBody(resp, err, &pr); err != nil {
		return 0, err
	}
	return pr.Version, nil
}

func (t *tools) resolveVersion(ctx context.Context, a PRArgs, v *int32) (int32, error) {
	if v != nil {
		return *v, nil
	}
	return t.currentVersion(ctx, a)
}

// register adds all tools to s. Write tools are skipped when readOnly is set.
func (t *tools) register(s *mcp.Server, readOnly bool) {
	read := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(true)}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_pull_requests",
		Description: "List pull requests in a repository. Returns a page (values, isLastPage, nextPageStart) of PR summaries without descriptions; use get_pull_request for the full PR.",
		Annotations: read,
	}, t.listPullRequests)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "list_my_pull_requests",
		Description: "List pull requests across all repositories that involve the authenticated user (the Bitbucket dashboard). Returns PR summaries without descriptions.",
		Annotations: read,
	}, t.listMyPullRequests)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request",
		Description: "Get a pull request, including its title, description, branches, reviewers, state and version.",
		Annotations: read,
	}, t.getPullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request_diff",
		Description: "Get the unified diff of a pull request as plain text. Output over 1 MiB is truncated.",
		Annotations: read,
	}, t.getPullRequestDiff)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request_changes",
		Description: "List the files changed by a pull request.",
		Annotations: read,
	}, t.getPullRequestChanges)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request_commits",
		Description: "List the commits of a pull request.",
		Annotations: read,
	}, t.getPullRequestCommits)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request_activities",
		Description: "List the activity of a pull request, newest first: comments (with replies), approvals, reviews, rescopes, merges and declines.",
		Annotations: read,
	}, t.getPullRequestActivities)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "get_pull_request_merge_status",
		Description: "Check whether a pull request can be merged, and list any vetoes (e.g. missing approvals, failing builds, conflicts).",
		Annotations: read,
	}, t.getPullRequestMergeStatus)

	if readOnly {
		return
	}

	write := &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(false)}
	idempotent := &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(false), IdempotentHint: true}
	destructive := &mcp.ToolAnnotations{OpenWorldHint: new(true), DestructiveHint: new(true)}

	mcp.AddTool(s, &mcp.Tool{
		Name:        "create_pull_request",
		Description: "Create a pull request from one branch to another.",
		Annotations: write,
	}, t.createPullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "update_pull_request",
		Description: "Update a pull request's title, description, target branch, reviewers or draft status. Fields that are not given are left unchanged.",
		Annotations: idempotent,
	}, t.updatePullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "add_pull_request_comment",
		Description: "Add a comment to a pull request: a general comment, a reply to another comment, or an inline comment on a file and line. Set severity to BLOCKER to create a task.",
		Annotations: write,
	}, t.addPullRequestComment)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "approve_pull_request",
		Description: "Approve a pull request as the authenticated user.",
		Annotations: idempotent,
	}, t.approvePullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "unapprove_pull_request",
		Description: "Remove the authenticated user's approval from a pull request.",
		Annotations: idempotent,
	}, t.unapprovePullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "set_pull_request_review_status",
		Description: "Set a participant's review status on a pull request: APPROVED, NEEDS_WORK or UNAPPROVED. Usually used with the authenticated user's own slug.",
		Annotations: idempotent,
	}, t.setReviewStatus)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "decline_pull_request",
		Description: "Decline an open pull request.",
		Annotations: destructive,
	}, t.declinePullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "reopen_pull_request",
		Description: "Reopen a declined pull request.",
		Annotations: write,
	}, t.reopenPullRequest)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "merge_pull_request",
		Description: "Merge a pull request. Use get_pull_request_merge_status first to check for vetoes.",
		Annotations: destructive,
	}, t.mergePullRequest)
}

// ---- read tools ----

type ListPullRequestsArgs struct {
	RepoArgs
	PageArgs
	RawArgs
	State      string `json:"state,omitempty" jsonschema:"OPEN (default), DECLINED, MERGED or ALL"`
	Direction  string `json:"direction,omitempty" jsonschema:"INCOMING (default, PRs targeting this repo) or OUTGOING (PRs from this repo)"`
	At         string `json:"at,omitempty" jsonschema:"Only PRs to (INCOMING) or from (OUTGOING) this fully qualified ref, e.g. refs/heads/main"`
	Order      string `json:"order,omitempty" jsonschema:"OLDEST or NEWEST (default)"`
	FilterText string `json:"filter_text,omitempty" jsonschema:"Only PRs whose title or description contains this text"`
	Draft      *bool  `json:"draft,omitempty" jsonschema:"true for only drafts, false for only non-drafts; omit for both"`
}

func (t *tools) listPullRequests(ctx context.Context, _ *mcp.CallToolRequest, a ListPullRequestsArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.GetPage(ctx, a.ProjectKey, a.RepositorySlug, &bitbucket.GetPageParams{
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
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, compactPage(toPullRequestSummary))
	return res, nil, err
}

type ListMyPullRequestsArgs struct {
	PageArgs
	RawArgs
	Role              string `json:"role,omitempty" jsonschema:"Only PRs where the user has this role: AUTHOR, REVIEWER or PARTICIPANT"`
	State             string `json:"state,omitempty" jsonschema:"OPEN, DECLINED or MERGED; omit for any state"`
	ParticipantStatus string `json:"participant_status,omitempty" jsonschema:"Comma separated participant statuses: UNAPPROVED, NEEDS_WORK, APPROVED"`
	Order             string `json:"order,omitempty" jsonschema:"OLDEST, NEWEST (default), DRAFT_STATUS, PARTICIPANT_STATUS or CLOSED_DATE"`
}

func (t *tools) listMyPullRequests(ctx context.Context, _ *mcp.CallToolRequest, a ListMyPullRequestsArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.GetPullRequests1(ctx, &bitbucket.GetPullRequests1Params{
		Role:              optStr(a.Role),
		State:             optStr(a.State),
		ParticipantStatus: optStr(a.ParticipantStatus),
		Order:             optStr(a.Order),
		Start:             a.start(),
		Limit:             a.limit(),
	}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, compactPage(toPullRequestSummary))
	return res, nil, err
}

type GetPullRequestArgs struct {
	PRArgs
	RawArgs
}

func (t *tools) getPullRequest(ctx context.Context, _ *mcp.CallToolRequest, a GetPullRequestArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.Get3(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, toPullRequest)
	return res, nil, err
}

type GetDiffArgs struct {
	PRArgs
	ContextLines int    `json:"context_lines,omitempty" jsonschema:"Lines of context around each change (Bitbucket default 10)"`
	Whitespace   string `json:"whitespace,omitempty" jsonschema:"Set to ignore-all to ignore whitespace changes"`
}

func (t *tools) getPullRequestDiff(ctx context.Context, _ *mcp.CallToolRequest, a GetDiffArgs) (*mcp.CallToolResult, any, error) {
	params := &bitbucket.StreamRawDiff2Params{Whitespace: optStr(a.Whitespace)}
	if a.ContextLines > 0 {
		params.ContextLines = optStr(strconv.Itoa(a.ContextLines))
	}
	body, err := readBody(t.c.StreamRawDiff2(ctx, a.ProjectKey, a.RepositorySlug, a.id(), params, acceptText))
	if err != nil {
		return nil, nil, err
	}
	if len(body) == 0 {
		return textResult("(empty diff)"), nil, nil
	}
	return textResult(string(body)), nil, nil
}

type GetChangesArgs struct {
	PRArgs
	PageArgs
	RawArgs
	ChangeScope string `json:"change_scope,omitempty" jsonschema:"ALL (default), UNREVIEWED (changes since the user's last review) or RANGE (use since_id and until_id)"`
	SinceID     string `json:"since_id,omitempty" jsonschema:"With change_scope RANGE: the commit to diff from"`
	UntilID     string `json:"until_id,omitempty" jsonschema:"With change_scope RANGE: the commit to diff to"`
}

func (t *tools) getPullRequestChanges(ctx context.Context, _ *mcp.CallToolRequest, a GetChangesArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.StreamChanges1(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.StreamChanges1Params{
		ChangeScope: optStr(a.ChangeScope),
		SinceId:     optStr(a.SinceID),
		UntilId:     optStr(a.UntilID),
		Start:       a.start(),
		Limit:       a.limit(),
	}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, compactPage(toChange))
	return res, nil, err
}

type PagedPRArgs struct {
	PRArgs
	PageArgs
	RawArgs
}

func (t *tools) getPullRequestCommits(ctx context.Context, _ *mcp.CallToolRequest, a PagedPRArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.GetCommits1(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.GetCommits1Params{
		Start: a.start(),
		Limit: a.limit(),
	}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, compactPage(toCommit))
	return res, nil, err
}

func (t *tools) getPullRequestActivities(ctx context.Context, _ *mcp.CallToolRequest, a PagedPRArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.GetActivities(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.GetActivitiesParams{
		Start: a.start(),
		Limit: a.limit(),
	}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, a.Raw, compactPage(toActivity))
	return res, nil, err
}

func (t *tools) getPullRequestMergeStatus(ctx context.Context, _ *mcp.CallToolRequest, a PRArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.CanMerge(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return nil, nil, err
	}
	return jsonResult(body), nil, nil
}

// ---- write tools ----

type refJSON struct {
	ID         string    `json:"id"`
	Repository *repoJSON `json:"repository,omitempty"`
}

type repoJSON struct {
	Slug    string      `json:"slug"`
	Project projectJSON `json:"project"`
}

type projectJSON struct {
	Key string `json:"key"`
}

type reviewerJSON struct {
	User struct {
		Name string `json:"name"`
	} `json:"user"`
}

func reviewers(names []string) []reviewerJSON {
	out := make([]reviewerJSON, len(names))
	for i, n := range names {
		out[i].User.Name = n
	}
	return out
}

// branchRef turns "main" into "refs/heads/main" and leaves full refs alone.
func branchRef(b string) string {
	if len(b) >= 5 && b[:5] == "refs/" {
		return b
	}
	return "refs/heads/" + b
}

type CreatePullRequestArgs struct {
	RepoArgs
	Title       string   `json:"title" jsonschema:"Pull request title"`
	Description string   `json:"description,omitempty" jsonschema:"Pull request description (Markdown)"`
	FromBranch  string   `json:"from_branch" jsonschema:"Source branch, e.g. feature/x or refs/heads/feature/x"`
	ToBranch    string   `json:"to_branch" jsonschema:"Target branch, e.g. main"`
	Reviewers   []string `json:"reviewers,omitempty" jsonschema:"Usernames of reviewers to add"`
	Draft       bool     `json:"draft,omitempty" jsonschema:"Create the pull request as a draft"`
	FromProject string   `json:"from_project_key,omitempty" jsonschema:"Project key of the source repository, if it is a fork. Defaults to project_key."`
	FromRepo    string   `json:"from_repository_slug,omitempty" jsonschema:"Slug of the source repository, if it is a fork. Defaults to repository_slug."`
}

func (t *tools) createPullRequest(ctx context.Context, _ *mcp.CallToolRequest, a CreatePullRequestArgs) (*mcp.CallToolResult, any, error) {
	fromRepo := &repoJSON{Slug: a.RepositorySlug, Project: projectJSON{Key: a.ProjectKey}}
	if a.FromRepo != "" {
		fromRepo.Slug = a.FromRepo
	}
	if a.FromProject != "" {
		fromRepo.Project.Key = a.FromProject
	}
	payload := map[string]any{
		"title":     a.Title,
		"fromRef":   refJSON{ID: branchRef(a.FromBranch), Repository: fromRepo},
		"toRef":     refJSON{ID: branchRef(a.ToBranch), Repository: &repoJSON{Slug: a.RepositorySlug, Project: projectJSON{Key: a.ProjectKey}}},
		"reviewers": reviewers(a.Reviewers),
	}
	if a.Description != "" {
		payload["description"] = a.Description
	}
	if a.Draft {
		payload["draft"] = true
	}
	r, err := jsonReader(payload)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody(t.c.CreateWithBody(ctx, a.ProjectKey, a.RepositorySlug, jsonContentType, r))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toPullRequest)
	return res, nil, err
}

type UpdatePullRequestArgs struct {
	PRArgs
	Version     *int32    `json:"version,omitempty" jsonschema:"Expected pull request version, for optimistic locking. Defaults to the current version."`
	Title       *string   `json:"title,omitempty" jsonschema:"New title"`
	Description *string   `json:"description,omitempty" jsonschema:"New description (Markdown)"`
	ToBranch    *string   `json:"to_branch,omitempty" jsonschema:"New target branch"`
	Reviewers   *[]string `json:"reviewers,omitempty" jsonschema:"Complete new list of reviewer usernames; reviewers not listed are removed"`
	Draft       *bool     `json:"draft,omitempty" jsonschema:"Mark as draft (true) or ready for review (false)"`
}

func (t *tools) updatePullRequest(ctx context.Context, _ *mcp.CallToolRequest, a UpdatePullRequestArgs) (*mcp.CallToolResult, any, error) {
	// Bitbucket replaces reviewers wholesale on PUT, so start from the current
	// pull request to leave unspecified fields untouched.
	var current struct {
		Version     int32            `json:"version"`
		Title       string           `json:"title"`
		Description string           `json:"description"`
		Draft       bool             `json:"draft"`
		ToRef       json.RawMessage  `json:"toRef"`
		Reviewers   []map[string]any `json:"reviewers"`
	}
	resp, err := t.c.Get3(ctx, a.ProjectKey, a.RepositorySlug, a.id())
	if err := decodeBody(resp, err, &current); err != nil {
		return nil, nil, err
	}

	payload := map[string]any{
		"version":     current.Version,
		"title":       current.Title,
		"description": current.Description,
		"draft":       current.Draft,
		"toRef":       current.ToRef,
	}
	// Send reviewers back as {user:{name}} only; the full participant objects
	// carry read-only fields.
	var keep []reviewerJSON
	for _, r := range current.Reviewers {
		if u, ok := r["user"].(map[string]any); ok {
			if name, ok := u["name"].(string); ok {
				keep = append(keep, reviewers([]string{name})...)
			}
		}
	}
	payload["reviewers"] = keep

	if a.Version != nil {
		payload["version"] = *a.Version
	}
	if a.Title != nil {
		payload["title"] = *a.Title
	}
	if a.Description != nil {
		payload["description"] = *a.Description
	}
	if a.Draft != nil {
		payload["draft"] = *a.Draft
	}
	if a.ToBranch != nil {
		payload["toRef"] = refJSON{
			ID:         branchRef(*a.ToBranch),
			Repository: &repoJSON{Slug: a.RepositorySlug, Project: projectJSON{Key: a.ProjectKey}},
		}
	}
	if a.Reviewers != nil {
		payload["reviewers"] = reviewers(*a.Reviewers)
	}

	r, err := jsonReader(payload)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody(t.c.UpdateWithBody(ctx, a.ProjectKey, a.RepositorySlug, a.id(), jsonContentType, r))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toPullRequest)
	return res, nil, err
}

type AddCommentArgs struct {
	PRArgs
	Text     string `json:"text" jsonschema:"Comment text (Markdown)"`
	ParentID int64  `json:"parent_id,omitempty" jsonschema:"ID of the comment to reply to"`
	Severity string `json:"severity,omitempty" jsonschema:"NORMAL (default) or BLOCKER (creates a task)"`
	Path     string `json:"path,omitempty" jsonschema:"For an inline comment: path of the file, relative to the repository root"`
	Line     int    `json:"line,omitempty" jsonschema:"For an inline comment: line number. Omit to comment on the whole file."`
	LineType string `json:"line_type,omitempty" jsonschema:"For an inline comment on a line: ADDED (default), REMOVED or CONTEXT"`
}

func (t *tools) addPullRequestComment(ctx context.Context, _ *mcp.CallToolRequest, a AddCommentArgs) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{"text": a.Text}
	if a.ParentID != 0 {
		payload["parent"] = map[string]any{"id": a.ParentID}
	}
	if a.Severity != "" {
		payload["severity"] = a.Severity
	}
	if a.Path != "" {
		anchor := map[string]any{
			"path":     a.Path,
			"diffType": "EFFECTIVE",
		}
		if a.Line > 0 {
			lineType := a.LineType
			if lineType == "" {
				lineType = "ADDED"
			}
			fileType := "TO"
			if lineType == "REMOVED" {
				fileType = "FROM"
			}
			anchor["line"] = a.Line
			anchor["lineType"] = lineType
			anchor["fileType"] = fileType
		} else {
			anchor["fileType"] = "TO"
		}
		payload["anchor"] = anchor
	}
	r, err := jsonReader(payload)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody(t.c.CreateComment2WithBody(ctx, a.ProjectKey, a.RepositorySlug, a.id(), jsonContentType, r))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toComment)
	return res, nil, err
}

func (t *tools) approvePullRequest(ctx context.Context, _ *mcp.CallToolRequest, a PRArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.Approve(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toParticipant)
	return res, nil, err
}

func (t *tools) unapprovePullRequest(ctx context.Context, _ *mcp.CallToolRequest, a PRArgs) (*mcp.CallToolResult, any, error) {
	body, err := readBody(t.c.WithdrawApproval(ctx, a.ProjectKey, a.RepositorySlug, a.id()))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toParticipant)
	return res, nil, err
}

type SetReviewStatusArgs struct {
	PRArgs
	UserSlug string `json:"user_slug" jsonschema:"Slug of the participant whose status to set; must be the authenticated user"`
	Status   string `json:"status" jsonschema:"APPROVED, NEEDS_WORK or UNAPPROVED"`
}

func (t *tools) setReviewStatus(ctx context.Context, _ *mcp.CallToolRequest, a SetReviewStatusArgs) (*mcp.CallToolResult, any, error) {
	status := bitbucket.RestPullRequestAssignStatusRequestStatus(a.Status)
	body, err := readBody(t.c.UpdateStatus(ctx, a.ProjectKey, a.RepositorySlug, a.id(), a.UserSlug,
		&bitbucket.UpdateStatusParams{},
		bitbucket.UpdateStatusJSONRequestBody{Status: &status}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toParticipant)
	return res, nil, err
}

type DeclineArgs struct {
	PRArgs
	Version *int32 `json:"version,omitempty" jsonschema:"Expected pull request version. Defaults to the current version."`
	Comment string `json:"comment,omitempty" jsonschema:"Optional reason for declining"`
}

func (t *tools) declinePullRequest(ctx context.Context, _ *mcp.CallToolRequest, a DeclineArgs) (*mcp.CallToolResult, any, error) {
	v, err := t.resolveVersion(ctx, a.PRArgs, a.Version)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody(t.c.Decline(ctx, a.ProjectKey, a.RepositorySlug, a.id(),
		&bitbucket.DeclineParams{},
		bitbucket.DeclineJSONRequestBody{Version: &v, Comment: optStr(a.Comment)}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toPullRequest)
	return res, nil, err
}

type ReopenArgs struct {
	PRArgs
	Version *int32 `json:"version,omitempty" jsonschema:"Expected pull request version. Defaults to the current version."`
}

func (t *tools) reopenPullRequest(ctx context.Context, _ *mcp.CallToolRequest, a ReopenArgs) (*mcp.CallToolResult, any, error) {
	v, err := t.resolveVersion(ctx, a.PRArgs, a.Version)
	if err != nil {
		return nil, nil, err
	}
	body, err := readBody(t.c.Reopen(ctx, a.ProjectKey, a.RepositorySlug, a.id(),
		&bitbucket.ReopenParams{},
		bitbucket.ReopenJSONRequestBody{Version: &v}))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toPullRequest)
	return res, nil, err
}

type MergeArgs struct {
	PRArgs
	Version    *int32 `json:"version,omitempty" jsonschema:"Expected pull request version. Defaults to the current version."`
	Message    string `json:"message,omitempty" jsonschema:"Merge commit message"`
	StrategyID string `json:"strategy_id,omitempty" jsonschema:"Merge strategy, e.g. no-ff, ff, ff-only, squash, rebase-no-ff. Defaults to the repository setting."`
	AutoMerge  bool   `json:"auto_merge,omitempty" jsonschema:"Merge automatically once all merge checks pass, instead of now"`
}

func (t *tools) mergePullRequest(ctx context.Context, _ *mcp.CallToolRequest, a MergeArgs) (*mcp.CallToolResult, any, error) {
	v, err := t.resolveVersion(ctx, a.PRArgs, a.Version)
	if err != nil {
		return nil, nil, err
	}
	req := bitbucket.MergeJSONRequestBody{
		Version:    &v,
		Message:    optStr(a.Message),
		StrategyId: optStr(a.StrategyID),
	}
	if a.AutoMerge {
		req.AutoMerge = &a.AutoMerge
	}
	body, err := readBody(t.c.Merge(ctx, a.ProjectKey, a.RepositorySlug, a.id(), &bitbucket.MergeParams{}, req))
	if err != nil {
		return nil, nil, err
	}
	res, err := compactResult(body, false, toPullRequest)
	return res, nil, err
}

// acceptText asks for a plain-text response, used for raw diffs.
func acceptText(_ context.Context, req *http.Request) error {
	req.Header.Set("Accept", "text/plain")
	return nil
}
