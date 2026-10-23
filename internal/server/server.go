// Package server exposes the Bitbucket Data Center pull request API as MCP tools.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cornelius-99/bitbucket-mcp/internal/bitbucket"
)

// Options configures the MCP server.
type Options struct {
	// Version is reported to MCP clients.
	Version string
	// ReadOnly hides every tool that changes state in Bitbucket.
	ReadOnly bool
}

const instructions = `Tools for pull requests on a Bitbucket Data Center / Server instance.
Repositories are addressed by project_key and repository_slug, as in the URL
https://<host>/projects/<project_key>/repos/<repository_slug>. List results are
paged: pass nextPageStart as start to get the next page while isLastPage is false.`

// New returns an MCP server whose tools call Bitbucket through c.
func New(c *bitbucket.Client, opts Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "bitbucket-mcp",
		Title:   "Bitbucket Data Center pull requests",
		Version: opts.Version,
	}, &mcp.ServerOptions{Instructions: instructions})
	(&tools{c: c}).register(s, opts.ReadOnly)
	return s
}
