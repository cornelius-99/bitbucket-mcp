package server

import (
	"encoding/json"
	"strings"
	"testing"
)

const testUser = `{"name":"jdoe","emailAddress":"jdoe@example.com","active":true,"displayName":"John Doe","id":101,"slug":"jdoe","type":"NORMAL","links":{"self":[{"href":"https://bb.example.com/users/jdoe"}]}}`

const testRepo = `{"slug":"repo","id":1,"name":"repo","hierarchyId":"e3c939f9ef4a7fae272e","scmId":"git","state":"AVAILABLE","statusMessage":"Available","forkable":true,
	"project":{"key":"PRJ","id":1,"name":"Project","public":false,"type":"NORMAL","links":{"self":[{"href":"https://bb.example.com/projects/PRJ"}]}},
	"public":false,"archived":false,"links":{"clone":[{"href":"ssh://git@bb.example.com:7999/prj/repo.git","name":"ssh"}],"self":[{"href":"https://bb.example.com/projects/PRJ/repos/repo/browse"}]}}`

const testPR = `{"id":7,"version":3,"title":"Add x","description":"Adds <x> & y","state":"OPEN","open":true,"closed":false,"draft":false,
	"createdDate":1761555000000,"updatedDate":1761558600000,
	"fromRef":{"id":"refs/heads/feature/x","displayId":"feature/x","latestCommit":"aaaa","type":"BRANCH","repository":` + testRepo + `},
	"toRef":{"id":"refs/heads/main","displayId":"main","latestCommit":"bbbb","type":"BRANCH","repository":` + testRepo + `},
	"locked":false,
	"author":{"user":` + testUser + `,"role":"AUTHOR","approved":false,"status":"UNAPPROVED"},
	"reviewers":[{"user":{"name":"bob","slug":"bob","displayName":"Bob","emailAddress":"bob@example.com"},"lastReviewedCommit":"aaaa","role":"REVIEWER","approved":true,"status":"APPROVED"}],
	"participants":[],
	"properties":{"mergeResult":{"outcome":"CLEAN","current":true},"resolvedTaskCount":0,"commentCount":2,"openTaskCount":1},
	"links":{"self":[{"href":"https://bb.example.com/projects/PRJ/repos/repo/pull-requests/7"}]}}`

func TestGetPullRequestCompact(t *testing.T) {
	_, cs := setup(t, map[string]string{"GET " + prPath: testPR}, Options{})

	out, isErr := call(t, cs, "get_pull_request", prArgs(nil))
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	var got pullRequest
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %s", out)
	}
	want := pullRequest{
		ID: 7, Version: 3, Repo: "PRJ/repo", Title: "Add x", Description: "Adds <x> & y", State: "OPEN",
		Author:    "jdoe (John Doe)",
		From:      ref{Branch: "feature/x", Commit: "aaaa"},
		To:        ref{Branch: "main", Commit: "bbbb"},
		Reviewers: []participant{{User: "bob (Bob)", Status: "APPROVED"}},
		Created:   "2025-10-27T08:50:00Z", Updated: "2025-10-27T09:50:00Z",
		MergeResult: "CLEAN", Comments: 2, OpenTasks: 1,
		URL: "https://bb.example.com/projects/PRJ/repos/repo/pull-requests/7",
	}
	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Errorf("got  %s\nwant %s", g, w)
	}
	if !strings.Contains(out, "<x> & y") {
		t.Errorf("HTML characters escaped: %s", out)
	}
	for _, noise := range []string{"emailAddress", "links", "hierarchyId", "ssh://"} {
		if strings.Contains(out, noise) {
			t.Errorf("output contains %q: %s", noise, out)
		}
	}
	if len(out)*3 > len(testPR) {
		t.Errorf("compact output is %d bytes, raw is %d", len(out), len(testPR))
	}
}

func TestRawReturnsBitbucketJSON(t *testing.T) {
	_, cs := setup(t, map[string]string{"GET " + prPath: testPR}, Options{})

	out, isErr := call(t, cs, "get_pull_request", prArgs(map[string]any{"raw": true}))
	if isErr || out != testPR {
		t.Errorf("raw output = %s (error %v)", out, isErr)
	}
}

func TestListPullRequestsOmitsDescription(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"GET /rest/api/latest/projects/PRJ/repos/repo/pull-requests": `{"size":1,"limit":25,"start":0,"isLastPage":false,"nextPageStart":1,"values":[` + testPR + `]}`,
	}, Options{})

	out, _ := call(t, cs, "list_pull_requests", map[string]any{"project_key": "PRJ", "repository_slug": "repo"})
	var got page[pullRequest]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %s", out)
	}
	if len(got.Values) != 1 || got.Values[0].Title != "Add x" || got.Values[0].Description != "" {
		t.Errorf("values = %+v", got.Values)
	}
	if got.IsLastPage || got.NextPageStart == nil || *got.NextPageStart != 1 {
		t.Errorf("paging = %v %v", got.IsLastPage, got.NextPageStart)
	}
	if strings.Contains(out, `"size"`) {
		t.Errorf("output keeps size: %s", out)
	}
}

func TestActivitiesCompact(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"GET " + prPath + "/activities": `{"isLastPage":true,"values":[
			{"id":1,"createdDate":1761555000000,"user":` + testUser + `,"action":"COMMENTED","commentAction":"ADDED",
			 "comment":{"properties":{"repositoryId":1},"id":50,"version":0,"text":"Why?","author":` + testUser + `,
			  "createdDate":1761555000000,"updatedDate":1761555000000,"severity":"NORMAL","state":"OPEN",
			  "permittedOperations":{"editable":true},
			  "comments":[{"id":51,"version":1,"text":"Because.","author":{"name":"bob","slug":"bob","displayName":"Bob"},"createdDate":1761555060000,"updatedDate":1761555120000,"severity":"NORMAL","state":"OPEN","comments":[]}]},
			 "commentAnchor":{"fromHash":"a","toHash":"b","line":42,"lineType":"ADDED","fileType":"TO","path":"src/a.go","diffType":"EFFECTIVE"}},
			{"id":2,"createdDate":1761555000000,"user":` + testUser + `,"action":"REVIEWED","participant":{"user":` + testUser + `,"status":"NEEDS_WORK"}},
			{"id":3,"createdDate":1761555000000,"user":` + testUser + `,"action":"RESCOPED",
			 "added":{"commits":[{"id":"cccccccccccc","displayId":"ccccccc","author":` + testUser + `,"message":"Fix it\n\nLonger body","parents":[{"id":"d"}]}],"total":1},
			 "removed":{"commits":[],"total":0}}
		]}`,
	}, Options{})

	out, isErr := call(t, cs, "get_pull_request_activities", prArgs(nil))
	if isErr {
		t.Fatalf("unexpected error: %s", out)
	}
	var got page[activity]
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("output is not JSON: %s", out)
	}
	if len(got.Values) != 3 {
		t.Fatalf("values = %s", out)
	}
	c := got.Values[0].Comment
	if c == nil || c.Text != "Why?" || c.File == nil || c.File.Path != "src/a.go" || c.File.Line != 42 {
		t.Errorf("comment = %+v", c)
	}
	if len(c.Replies) != 1 || c.Replies[0].Author != "bob (Bob)" || c.Replies[0].Edited == "" {
		t.Errorf("replies = %+v", c.Replies)
	}
	if got.Values[1].Status != "NEEDS_WORK" {
		t.Errorf("review status = %q", got.Values[1].Status)
	}
	if a := got.Values[2].AddedCommits; len(a) != 1 || a[0] != "ccccccc Fix it" {
		t.Errorf("added commits = %v", a)
	}
}

func TestChangesCompact(t *testing.T) {
	_, cs := setup(t, map[string]string{
		"GET " + prPath + "/changes": `{"fromHash":"a","toHash":"b","isLastPage":true,"values":[
			{"contentId":"x","path":{"components":["src","b.go"],"parent":"src","name":"b.go","extension":"go","toString":"src/b.go"},
			 "srcPath":{"toString":"src/a.go"},"type":"MOVE","nodeType":"FILE","links":{"self":[null]}}]}`,
	}, Options{})

	out, _ := call(t, cs, "get_pull_request_changes", prArgs(nil))
	if want := `{"values":[{"path":"src/b.go","srcPath":"src/a.go","type":"MOVE"}],"isLastPage":true}`; out != want {
		t.Errorf("output = %s\nwant     %s", out, want)
	}
}

func TestUserName(t *testing.T) {
	for _, tc := range []struct {
		u    bbUser
		want string
	}{
		{bbUser{Slug: "jdoe", DisplayName: "John Doe"}, "jdoe (John Doe)"},
		{bbUser{Slug: "jdoe", DisplayName: "jdoe"}, "jdoe"},
		{bbUser{Name: "Git Author", EmailAddress: "git@example.com"}, "Git Author"},
		{bbUser{EmailAddress: "git@example.com"}, "git@example.com"},
	} {
		if got := userName(tc.u); got != tc.want {
			t.Errorf("userName(%+v) = %q, want %q", tc.u, got, tc.want)
		}
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
