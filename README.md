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

### With mise

[mise](https://mise.jdx.dev) can build and install the server through its `go:` backend. It needs Go 1.27 or newer, which mise can also provide:

```sh
mise use -g go@1.27
mise use -g go:github.com/cornelius-99/bitbucket-mcp@latest
```

This puts `bitbucket-mcp` on your PATH through mise's shims (or `mise activate`). You can pin a version per project instead by running the second command without `-g` in that project, which writes it to `mise.toml`:

```toml
[tools]
"go:github.com/cornelius-99/bitbucket-mcp" = "latest"
```

If your MCP client does not see mise's PATH, point it at the binary with `mise exec`:

```json
{
  "mcpServers": {
    "bitbucket": {
      "command": "mise",
      "args": ["exec", "go:github.com/cornelius-99/bitbucket-mcp@latest", "--", "bitbucket-mcp"],
      "env": {
        "BITBUCKET_URL": "https://bitbucket.example.com",
        "BITBUCKET_TOKEN": "..."
      }
    }
  }
}
```

Upgrade with `mise upgrade go:github.com/cornelius-99/bitbucket-mcp`.

### With Go

```sh
go install github.com/cornelius-99/bitbucket-mcp@latest
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

### opencode

Add the server under `mcp` in your [opencode](https://opencode.ai) config. That is `~/.config/opencode/opencode.json` for all projects, or `opencode.json` in a project root for one project. `{env:...}` reads the value from your shell environment, so the token stays out of the file:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "bitbucket": {
      "type": "local",
      "command": ["bitbucket-mcp"],
      "enabled": true,
      "environment": {
        "BITBUCKET_URL": "https://bitbucket.example.com",
        "BITBUCKET_TOKEN": "{env:BITBUCKET_TOKEN}"
      }
    }
  }
}
```

Run `opencode mcp list` to check that the server connects.

### Streamable HTTP

```sh
BITBUCKET_URL=... BITBUCKET_TOKEN=... bitbucket-mcp -http :8080
```

The HTTP endpoint has no authentication of its own and acts with the configured token, so only bind it to a trusted network.

## Development

```
api/                            Atlassian's spec, downloaded by `make generate` (not committed)
internal/tools/specfilter       keeps /api/latest/**/pull-requests* paths, prunes unused schemas
internal/bitbucket              oapi-codegen config + generated client (client.gen.go, committed)
internal/server                 MCP tools on top of the generated client
main.go                         entry point
```

| Command | |
| --- | --- |
| `make generate` | Download the official spec (`SPEC_VERSION`, default 9.4), filter it to the pull request API and regenerate the client |
| `make test` | `go vet` and `go test` |
| `make build` | Build `bin/bitbucket-mcp` |

To target another Bitbucket version: `make generate SPEC_VERSION=8.19`.

The **Regenerate client** workflow (Actions tab, input `spec_version`) does the same on GitHub and opens a PR when the generated client changes.

### Releasing

Run the **Release** workflow from the Actions tab with a version such as `v0.1.2` (or `gh workflow run release.yml -f version=v0.1.2`). It runs the tests on `main`, creates the tag and a GitHub Release with generated notes. `go install` and mise pick up the new version from the Go module proxy.

## License

The code in this repository is licensed under [MIT](LICENSE).

The Bitbucket REST API definitions (the OpenAPI spec) belong to Atlassian. They are not part of this repository and are not covered by the MIT license: `make generate` downloads them from [Atlassian's developer site](https://developer.atlassian.com/server/bitbucket/rest/) when the client is regenerated.
