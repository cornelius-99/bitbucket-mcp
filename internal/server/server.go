// Package server exposes the Bitbucket Data Center pull request API over MCP:
// reads as tools and resources, changes as tools.
package server

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cornelius-99/bitbucket-mcp/internal/bitbucket"
)

// Options configures the MCP server.
type Options struct {
	// Version is reported to MCP clients.
	Version string
	// ReadOnly registers only the reads, leaving out every tool that changes
	// state in Bitbucket.
	ReadOnly bool
}

const instructions = `Pull requests on a Bitbucket Data Center / Server instance.
Repositories are addressed by project_key and repository_slug, as in the URL
https://<host>/projects/<project_key>/repos/<repository_slug>. Every read tool is
also a resource whose URI mirrors the web URL:
bitbucket://projects/<project_key>/repos/<repository_slug>/pull-requests/<id>,
with /diff, /changes, /commits, /activities and /merge-status below a pull request,
and bitbucket://dashboard/pull-requests for your own. Resource parameters go in the
query string, percent-encoded (e.g. ?at=refs%2Fheads%2Fmain&limit=10). List results
are paged: pass nextPageStart as start to get the next page while isLastPage is false.
Results are compact summaries: users are "slug (Display Name)" and times are
RFC 3339. Pass raw: true to a read tool (raw=true on a JSON resource) to get
Bitbucket's full JSON instead.`

// New returns an MCP server whose resources and tools call Bitbucket through c.
func New(c *bitbucket.Client, opts Options) *mcp.Server {
	s := mcp.NewServer(&mcp.Implementation{
		Name:    "bitbucket-mcp",
		Title:   "Bitbucket Data Center pull requests",
		Version: opts.Version,
	}, &mcp.ServerOptions{Instructions: instructions})
	(&reads{c: c}).register(s)
	if !opts.ReadOnly {
		(&tools{c: c}).register(s)
	}
	return s
}
