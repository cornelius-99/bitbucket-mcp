# bitbucket-mcp

An [MCP](https://modelcontextprotocol.io) server for the **Bitbucket Data Center / Server pull request API**, written in Go.

- The Bitbucket client is generated with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) from Atlassian's official OpenAPI spec, filtered down to the pull request endpoints.
- The MCP server is built on the official [Go SDK](https://github.com/modelcontextprotocol/go-sdk).
- It authenticates with a Bitbucket **HTTP access token** (personal access token), sent as `Authorization: Bearer <token>`.

## Tools

| Tool | What it does |
| --- | --- |
| `list_pull_requests` | List PRs in a repository (state, direction, branch, text filter, paging) |
| `list_my_pull_requests` | PRs involving you across all repositories (dashboard) |
| `get_pull_request` | One PR: title, description, refs, reviewers, state, version |
| `get_pull_request_diff` | Unified diff as text (truncated above 1 MiB) |
| `get_pull_request_changes` | Files changed |
| `get_pull_request_commits` | Commits in the PR |
| `get_pull_request_activities` | Comments, approvals, rescopes, merges |
| `get_pull_request_merge_status` | Whether it can merge, and any vetoes |
| `create_pull_request` | Open a PR (supports forks, reviewers, drafts) |
| `update_pull_request` | Change title, description, target, reviewers, draft; unspecified fields are kept |
| `add_pull_request_comment` | General, reply, inline (file/line) comment, or task (`severity: BLOCKER`) |
| `approve_pull_request` / `unapprove_pull_request` | Approve or withdraw approval |
| `set_pull_request_review_status` | `APPROVED`, `NEEDS_WORK` or `UNAPPROVED` |
| `decline_pull_request` / `reopen_pull_request` | Decline or reopen |
| `merge_pull_request` | Merge, optionally with a strategy or auto-merge |

Tools that take a `version` use the PR's current version when it is omitted.
Start with `-read-only` (or `BITBUCKET_READ_ONLY=true`) to expose only the read tools.

## Configuration

| Variable | Required | Description |
| --- | --- | --- |
| `BITBUCKET_URL` | yes | Base URL of your instance, e.g. `https://bitbucket.example.com` (include any context path) |
| `BITBUCKET_TOKEN` | yes | HTTP access token. Create one under *Manage account → HTTP access tokens*; it needs at least *Repository read* (and *Repository write* for the write tools). |
| `BITBUCKET_READ_ONLY` | no | `true` to register only read tools |

## Install

```sh
go install github.com/cornelius-99/bitbucket-mcp/cmd/bitbucket-mcp@latest
```

or `make build`, which writes `bin/bitbucket-mcp`.

## Use

### stdio (default)

Claude Code:

```sh
claude mcp add bitbucket \
  -e BITBUCKET_URL=https://bitbucket.example.com \
  -e BITBUCKET_TOKEN=... \
  -- bitbucket-mcp
```

Generic MCP client config:

```json
{
  "mcpServers": {
    "bitbucket": {
      "command": "bitbucket-mcp",
      "env": {
        "BITBUCKET_URL": "https://bitbucket.example.com",
        "BITBUCKET_TOKEN": "..."
      }
    }
  }
}
```

### Streamable HTTP

```sh
BITBUCKET_URL=... BITBUCKET_TOKEN=... bitbucket-mcp -http :8080
```

The HTTP endpoint has no authentication of its own and acts with the configured token, so only bind it to a trusted network.

## Development

```
api/pullrequests.openapi.json   filtered spec (committed)
internal/tools/specfilter       keeps /api/latest/**/pull-requests* paths, prunes unused schemas
internal/bitbucket              oapi-codegen config + generated client (client.gen.go)
internal/server                 MCP tools on top of the generated client
cmd/bitbucket-mcp               entry point
```

| Command | |
| --- | --- |
| `make regenerate` | Download the official spec (`SPEC_VERSION`, default 9.4), filter it, regenerate the client |
| `make generate` | Regenerate the client from the committed filtered spec |
| `make test` | `go vet` and `go test` |
| `make build` | Build `bin/bitbucket-mcp` |

To target another Bitbucket version: `make regenerate SPEC_VERSION=8.19`.
