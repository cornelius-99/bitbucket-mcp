package server

import (
	"bytes"
	"encoding/json"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Bitbucket's JSON repeats full user, repository and project objects (with
// links) on every item, so most of a raw response is noise to a model. Read
// tools therefore decode the fields that matter into the compact types below
// and return those, unless the caller passes raw: true.

// RawArgs lets a caller ask for Bitbucket's unmodified response.
type RawArgs struct {
	Raw bool `json:"raw,omitempty" jsonschema:"Return Bitbucket's full, unmodified JSON instead of the compact summary"`
}

// compactResult decodes body as In and returns conv's result as JSON, or body
// itself when raw is set.
func compactResult[In, Out any](body []byte, raw bool, conv func(In) Out) (*mcp.CallToolResult, error) {
	if raw || len(bytes.TrimSpace(body)) == 0 {
		return jsonResult(body), nil
	}
	var in In
	if err := json.Unmarshal(body, &in); err != nil {
		// Not the expected shape (or truncated): better the raw body than nothing.
		return jsonResult(body), nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // keep <, > and & in descriptions and comments readable
	if err := enc.Encode(conv(in)); err != nil {
		return nil, err
	}
	return textResult(strings.TrimSuffix(buf.String(), "\n")), nil
}

// ---- Bitbucket JSON, only the fields that are kept ----

type bbPage[T any] struct {
	Values        []T  `json:"values"`
	IsLastPage    bool `json:"isLastPage"`
	NextPageStart *int `json:"nextPageStart"`
}

type bbUser struct {
	Name         string `json:"name"`
	EmailAddress string `json:"emailAddress"`
	DisplayName  string `json:"displayName"`
	Slug         string `json:"slug"`
}

type bbParticipant struct {
	User   bbUser `json:"user"`
	Status string `json:"status"`
}

type bbRef struct {
	DisplayID    string `json:"displayId"`
	LatestCommit string `json:"latestCommit"`
	Repository   struct {
		Slug    string `json:"slug"`
		Project struct {
			Key string `json:"key"`
		} `json:"project"`
	} `json:"repository"`
}

type bbLinks struct {
	Self []struct {
		Href string `json:"href"`
	} `json:"self"`
}

type bbPullRequest struct {
	ID           int64           `json:"id"`
	Version      int32           `json:"version"`
	Title        string          `json:"title"`
	Description  string          `json:"description"`
	State        string          `json:"state"`
	Draft        bool            `json:"draft"`
	CreatedDate  int64           `json:"createdDate"`
	UpdatedDate  int64           `json:"updatedDate"`
	ClosedDate   int64           `json:"closedDate"`
	FromRef      bbRef           `json:"fromRef"`
	ToRef        bbRef           `json:"toRef"`
	Author       bbParticipant   `json:"author"`
	Reviewers    []bbParticipant `json:"reviewers"`
	Participants []bbParticipant `json:"participants"`
	Properties   struct {
		MergeResult *struct {
			Outcome string `json:"outcome"`
		} `json:"mergeResult"`
		CommentCount      int `json:"commentCount"`
		OpenTaskCount     int `json:"openTaskCount"`
		ResolvedTaskCount int `json:"resolvedTaskCount"`
	} `json:"properties"`
	Links bbLinks `json:"links"`
}

type bbComment struct {
	ID             int64       `json:"id"`
	Version        int32       `json:"version"`
	Text           string      `json:"text"`
	Author         bbUser      `json:"author"`
	CreatedDate    int64       `json:"createdDate"`
	UpdatedDate    int64       `json:"updatedDate"`
	Severity       string      `json:"severity"`
	State          string      `json:"state"`
	ThreadResolved bool        `json:"threadResolved"`
	Anchor         *bbAnchor   `json:"anchor"`
	Comments       []bbComment `json:"comments"`
}

type bbAnchor struct {
	Path     string `json:"path"`
	Line     int    `json:"line"`
	LineType string `json:"lineType"`
}

type bbCommit struct {
	ID              string `json:"id"`
	DisplayID       string `json:"displayId"`
	Author          bbUser `json:"author"`
	AuthorTimestamp int64  `json:"authorTimestamp"`
	Message         string `json:"message"`
	Parents         []struct {
		ID string `json:"id"`
	} `json:"parents"`
}

type bbActivity struct {
	ID            int64          `json:"id"`
	Action        string         `json:"action"`
	CommentAction string         `json:"commentAction"`
	CreatedDate   int64          `json:"createdDate"`
	User          bbUser         `json:"user"`
	Comment       *bbComment     `json:"comment"`
	CommentAnchor *bbAnchor      `json:"commentAnchor"`
	Participant   *bbParticipant `json:"participant"`
	Added         *struct {
		Commits []bbCommit `json:"commits"`
	} `json:"added"`
	Removed *struct {
		Commits []bbCommit `json:"commits"`
	} `json:"removed"`
	AddedReviewers   []bbUser `json:"addedReviewers"`
	RemovedReviewers []bbUser `json:"removedReviewers"`
}

type bbPath struct {
	ToString string `json:"toString"`
}

type bbChange struct {
	Path    bbPath  `json:"path"`
	SrcPath *bbPath `json:"srcPath"`
	Type    string  `json:"type"`
}

// ---- compact output ----

type page[T any] struct {
	Values        []T  `json:"values"`
	IsLastPage    bool `json:"isLastPage"`
	NextPageStart *int `json:"nextPageStart,omitempty"`
}

func compactPage[In, Out any](conv func(In) Out) func(bbPage[In]) page[Out] {
	return func(p bbPage[In]) page[Out] {
		out := page[Out]{Values: make([]Out, len(p.Values)), IsLastPage: p.IsLastPage}
		for i, v := range p.Values {
			out.Values[i] = conv(v)
		}
		if !p.IsLastPage {
			out.NextPageStart = p.NextPageStart
		}
		return out
	}
}

type participant struct {
	User   string `json:"user"`
	Status string `json:"status"`
}

type ref struct {
	Branch string `json:"branch"`
	Commit string `json:"commit,omitempty"`
	// Repo is set on the source ref when it lives in another repository (a fork).
	Repo string `json:"repo,omitempty"`
}

type pullRequest struct {
	ID            int64         `json:"id"`
	Version       int32         `json:"version"`
	Repo          string        `json:"repo"`
	Title         string        `json:"title"`
	Description   string        `json:"description,omitempty"`
	State         string        `json:"state"`
	Draft         bool          `json:"draft,omitempty"`
	Author        string        `json:"author"`
	From          ref           `json:"from"`
	To            ref           `json:"to"`
	Reviewers     []participant `json:"reviewers"`
	Participants  []participant `json:"participants,omitempty"`
	Created       string        `json:"created"`
	Updated       string        `json:"updated"`
	Closed        string        `json:"closed,omitempty"`
	MergeResult   string        `json:"mergeResult,omitempty"`
	Comments      int           `json:"comments,omitempty"`
	OpenTasks     int           `json:"openTasks,omitempty"`
	ResolvedTasks int           `json:"resolvedTasks,omitempty"`
	URL           string        `json:"url,omitempty"`
}

type comment struct {
	ID       int64     `json:"id"`
	Version  int32     `json:"version"`
	Author   string    `json:"author"`
	Text     string    `json:"text"`
	Created  string    `json:"created"`
	Edited   string    `json:"edited,omitempty"`
	Severity string    `json:"severity,omitempty"`
	State    string    `json:"state,omitempty"`
	Resolved bool      `json:"threadResolved,omitempty"`
	File     *anchor   `json:"file,omitempty"`
	Replies  []comment `json:"replies,omitempty"`
}

type anchor struct {
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	LineType string `json:"lineType,omitempty"`
}

type commit struct {
	ID      string `json:"id"`
	Author  string `json:"author"`
	Date    string `json:"date"`
	Message string `json:"message"`
	Merge   bool   `json:"merge,omitempty"`
}

type activity struct {
	ID               int64    `json:"id"`
	Action           string   `json:"action"`
	CommentAction    string   `json:"commentAction,omitempty"`
	User             string   `json:"user"`
	Date             string   `json:"date"`
	Status           string   `json:"status,omitempty"`
	Comment          *comment `json:"comment,omitempty"`
	AddedCommits     []string `json:"addedCommits,omitempty"`
	RemovedCommits   []string `json:"removedCommits,omitempty"`
	AddedReviewers   []string `json:"addedReviewers,omitempty"`
	RemovedReviewers []string `json:"removedReviewers,omitempty"`
}

type change struct {
	Path    string `json:"path"`
	SrcPath string `json:"srcPath,omitempty"`
	Type    string `json:"type"`
}

// ---- conversions ----

// userName renders a user as "slug (Display Name)". Commit authors without a
// Bitbucket account only have a name and email.
func userName(u bbUser) string {
	switch {
	case u.Slug == "" && u.Name != "":
		return u.Name
	case u.Slug == "":
		return u.EmailAddress
	case u.DisplayName == "" || u.DisplayName == u.Slug:
		return u.Slug
	default:
		return u.Slug + " (" + u.DisplayName + ")"
	}
}

func userNames(us []bbUser) []string {
	if len(us) == 0 {
		return nil
	}
	out := make([]string, len(us))
	for i, u := range us {
		out[i] = userName(u)
	}
	return out
}

// timestamp turns Bitbucket's epoch milliseconds into RFC 3339, which models
// read reliably.
func timestamp(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339)
}

func toParticipant(p bbParticipant) participant {
	return participant{User: userName(p.User), Status: p.Status}
}

func toParticipants(ps []bbParticipant) []participant {
	out := make([]participant, len(ps))
	for i, p := range ps {
		out[i] = toParticipant(p)
	}
	return out
}

func repoName(r bbRef) string {
	if r.Repository.Slug == "" {
		return ""
	}
	return r.Repository.Project.Key + "/" + r.Repository.Slug
}

func toPullRequest(p bbPullRequest) pullRequest {
	out := pullRequest{
		ID:            p.ID,
		Version:       p.Version,
		Repo:          repoName(p.ToRef),
		Title:         p.Title,
		Description:   p.Description,
		State:         p.State,
		Draft:         p.Draft,
		Author:        userName(p.Author.User),
		From:          ref{Branch: p.FromRef.DisplayID, Commit: p.FromRef.LatestCommit},
		To:            ref{Branch: p.ToRef.DisplayID, Commit: p.ToRef.LatestCommit},
		Reviewers:     toParticipants(p.Reviewers),
		Created:       timestamp(p.CreatedDate),
		Updated:       timestamp(p.UpdatedDate),
		Closed:        timestamp(p.ClosedDate),
		Comments:      p.Properties.CommentCount,
		OpenTasks:     p.Properties.OpenTaskCount,
		ResolvedTasks: p.Properties.ResolvedTaskCount,
	}
	if from := repoName(p.FromRef); from != out.Repo {
		out.From.Repo = from
	}
	if len(p.Participants) > 0 {
		out.Participants = toParticipants(p.Participants)
	}
	if mr := p.Properties.MergeResult; mr != nil {
		out.MergeResult = mr.Outcome
	}
	if len(p.Links.Self) > 0 {
		out.URL = p.Links.Self[0].Href
	}
	return out
}

// toPullRequestSummary is toPullRequest for lists: the description and
// participants are left to get_pull_request.
func toPullRequestSummary(p bbPullRequest) pullRequest {
	out := toPullRequest(p)
	out.Description = ""
	out.Participants = nil
	return out
}

func toAnchor(a *bbAnchor) *anchor {
	if a == nil || a.Path == "" {
		return nil
	}
	return &anchor{Path: a.Path, Line: a.Line, LineType: a.LineType}
}

func toComment(c bbComment) comment {
	out := comment{
		ID:       c.ID,
		Version:  c.Version,
		Author:   userName(c.Author),
		Text:     c.Text,
		Created:  timestamp(c.CreatedDate),
		Severity: c.Severity,
		State:    c.State,
		Resolved: c.ThreadResolved,
		File:     toAnchor(c.Anchor),
	}
	if c.UpdatedDate != c.CreatedDate {
		out.Edited = timestamp(c.UpdatedDate)
	}
	for _, r := range c.Comments {
		out.Replies = append(out.Replies, toComment(r))
	}
	return out
}

func toCommit(c bbCommit) commit {
	return commit{
		ID:      c.ID,
		Author:  userName(c.Author),
		Date:    timestamp(c.AuthorTimestamp),
		Message: c.Message,
		Merge:   len(c.Parents) > 1,
	}
}

// commitLine renders a commit as "<short id> <subject>".
func commitLine(c bbCommit) string {
	id := c.DisplayID
	if id == "" {
		id = c.ID
	}
	subject, _, _ := strings.Cut(c.Message, "\n")
	return strings.TrimSpace(id + " " + subject)
}

func commitLines(cs []bbCommit) []string {
	if len(cs) == 0 {
		return nil
	}
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = commitLine(c)
	}
	return out
}

func toActivity(a bbActivity) activity {
	out := activity{
		ID:               a.ID,
		Action:           a.Action,
		CommentAction:    a.CommentAction,
		User:             userName(a.User),
		Date:             timestamp(a.CreatedDate),
		AddedReviewers:   userNames(a.AddedReviewers),
		RemovedReviewers: userNames(a.RemovedReviewers),
	}
	if a.Participant != nil {
		out.Status = a.Participant.Status
	}
	if a.Comment != nil {
		c := toComment(*a.Comment)
		if c.File == nil {
			c.File = toAnchor(a.CommentAnchor)
		}
		out.Comment = &c
	}
	if a.Added != nil {
		out.AddedCommits = commitLines(a.Added.Commits)
	}
	if a.Removed != nil {
		out.RemovedCommits = commitLines(a.Removed.Commits)
	}
	return out
}

func toChange(c bbChange) change {
	out := change{Path: c.Path.ToString, Type: c.Type}
	if c.SrcPath != nil && c.SrcPath.ToString != c.Path.ToString {
		out.SrcPath = c.SrcPath.ToString
	}
	return out
}
