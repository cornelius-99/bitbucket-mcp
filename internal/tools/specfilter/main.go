// Command specfilter reduces the full Bitbucket Data Center OpenAPI spec to the
// pull request API, so oapi-codegen only generates what this server uses.
//
// It keeps every /api/latest/ path that contains "/pull-requests" (except the
// admin and settings endpoints) and then drops every component schema that is
// no longer reachable from the kept operations.
//
// Usage:
//
//	specfilter -in api/bitbucket.swagger.v3.json -out api/pullrequests.openapi.json
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
)

func main() {
	in := flag.String("in", "", "path to the full Bitbucket OpenAPI v3 spec")
	out := flag.String("out", "", "path to write the filtered spec to")
	flag.Parse()
	if *in == "" || *out == "" {
		flag.Usage()
		os.Exit(2)
	}

	raw, err := os.ReadFile(*in)
	if err != nil {
		log.Fatal(err)
	}
	var spec map[string]any
	if err := json.Unmarshal(raw, &spec); err != nil {
		log.Fatalf("parse %s: %v", *in, err)
	}

	kept, err := filter(spec)
	if err != nil {
		log.Fatal(err)
	}

	data, err := json.MarshalIndent(spec, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "specfilter: kept %d paths\n", kept)
}

// keepPath reports whether a path belongs to the pull request API.
func keepPath(p string) bool {
	if !strings.HasPrefix(p, "/api/latest/") || !strings.Contains(p, "/pull-requests") {
		return false
	}
	if strings.HasPrefix(p, "/api/latest/admin/") || strings.Contains(p, "/settings/pull-requests") {
		return false
	}
	return true
}

// filter mutates spec in place and returns the number of kept paths.
func filter(spec map[string]any) (int, error) {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return 0, fmt.Errorf("spec has no paths object")
	}
	for p := range paths {
		if !keepPath(p) {
			delete(paths, p)
		}
	}
	if len(paths) == 0 {
		return 0, fmt.Errorf("no pull request paths found")
	}

	components, _ := spec["components"].(map[string]any)
	if components == nil {
		return len(paths), nil
	}

	// Walk refs transitively, starting from the kept paths.
	reachable := map[string]bool{} // "#/components/<kind>/<name>"
	queue := collectRefs(paths, nil)
	for len(queue) > 0 {
		ref := queue[0]
		queue = queue[1:]
		if reachable[ref] {
			continue
		}
		reachable[ref] = true
		if target := resolve(components, ref); target != nil {
			queue = collectRefs(target, queue)
		}
	}

	for kind, v := range components {
		group, ok := v.(map[string]any)
		if !ok || kind == "securitySchemes" {
			continue
		}
		for name := range group {
			if !reachable["#/components/"+kind+"/"+name] {
				delete(group, name)
			}
		}
	}

	// Bitbucket DC accepts HTTP access tokens as bearer tokens; the upstream
	// spec only declares basic auth.
	components["securitySchemes"] = map[string]any{
		"bearer": map[string]any{
			"type":        "http",
			"scheme":      "bearer",
			"description": "Bitbucket HTTP access token (personal access token).",
		},
	}
	spec["security"] = []any{map[string]any{"bearer": []any{}}}

	return len(paths), nil
}

func collectRefs(node any, acc []string) []string {
	switch n := node.(type) {
	case map[string]any:
		keys := make([]string, 0, len(n))
		for k := range n {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := n[k].(string); ok && k == "$ref" {
				acc = append(acc, s)
				continue
			}
			acc = collectRefs(n[k], acc)
		}
	case []any:
		for _, v := range n {
			acc = collectRefs(v, acc)
		}
	}
	return acc
}

func resolve(components map[string]any, ref string) any {
	parts := strings.Split(strings.TrimPrefix(ref, "#/components/"), "/")
	if len(parts) != 2 {
		return nil
	}
	group, _ := components[parts[0]].(map[string]any)
	return group[parts[1]]
}
