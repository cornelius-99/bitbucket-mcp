package main

import "testing"

func TestKeepPath(t *testing.T) {
	for p, want := range map[string]bool{
		"/api/latest/projects/{projectKey}/repos/{repositorySlug}/pull-requests":                        true,
		"/api/latest/projects/{projectKey}/repos/{repositorySlug}/pull-requests/{pullRequestId}/merge":  true,
		"/api/latest/dashboard/pull-requests":                                                           true,
		"/api/latest/admin/pull-requests/{scmId}":                                                       false,
		"/api/latest/projects/{projectKey}/repos/{repositorySlug}/settings/pull-requests":               false,
		"/git/latest/projects/{projectKey}/repos/{repositorySlug}/pull-requests/{pullRequestId}/rebase": false,
		"/api/latest/projects/{projectKey}/repos/{repositorySlug}/branches":                             false,
	} {
		if got := keepPath(p); got != want {
			t.Errorf("keepPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestFilterPrunesUnreachableSchemas(t *testing.T) {
	spec := map[string]any{
		"paths": map[string]any{
			"/api/latest/dashboard/pull-requests": map[string]any{
				"get": map[string]any{"responses": map[string]any{"200": map[string]any{
					"$ref": "#/components/schemas/Page",
				}}},
			},
			"/api/latest/users": map[string]any{},
		},
		"components": map[string]any{
			"schemas": map[string]any{
				"Page":   map[string]any{"properties": map[string]any{"v": map[string]any{"$ref": "#/components/schemas/PR"}}},
				"PR":     map[string]any{},
				"Unused": map[string]any{},
			},
		},
	}
	n, err := filter(spec)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("kept %d paths, want 1", n)
	}
	schemas := spec["components"].(map[string]any)["schemas"].(map[string]any)
	if _, ok := schemas["Unused"]; ok {
		t.Error("unreachable schema was kept")
	}
	if _, ok := schemas["PR"]; !ok {
		t.Error("transitively referenced schema was dropped")
	}
}
