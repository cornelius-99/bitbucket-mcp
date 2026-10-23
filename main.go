// Command bitbucket-mcp is an MCP server for the Bitbucket Data Center / Server
// pull request API.
//
// Configuration comes from the environment:
//
//	BITBUCKET_URL    base URL of the Bitbucket instance, e.g. https://bitbucket.example.com
//	BITBUCKET_TOKEN  HTTP access token (personal access token)
//
// By default the server speaks MCP over stdio. Pass -http to serve the
// streamable HTTP transport instead.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/cornelius-99/bitbucket-mcp/internal/server"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	httpAddr := flag.String("http", "", "serve streamable HTTP on this address (e.g. :8080) instead of stdio")
	readOnly := flag.Bool("read-only", envBool("BITBUCKET_READ_ONLY"), "only register tools that do not modify Bitbucket (env BITBUCKET_READ_ONLY)")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	// Logs go to stderr; stdout belongs to the stdio transport.
	log.SetOutput(os.Stderr)
	log.SetFlags(0)
	log.SetPrefix("bitbucket-mcp: ")

	client, err := server.NewClient(os.Getenv("BITBUCKET_URL"), os.Getenv("BITBUCKET_TOKEN"), "bitbucket-mcp/"+version)
	if err != nil {
		log.Fatalf("%v (set BITBUCKET_URL and BITBUCKET_TOKEN)", err)
	}
	srv := server.New(client, server.Options{Version: version, ReadOnly: *readOnly})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *httpAddr == "" {
		if err := srv.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
			log.Fatal(err)
		}
		return
	}

	handler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	httpSrv := &http.Server{Addr: *httpAddr, Handler: handler}
	go func() {
		<-ctx.Done()
		httpSrv.Close()
	}()
	log.Printf("serving streamable HTTP on %s", *httpAddr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func envBool(key string) bool {
	b, _ := strconv.ParseBool(os.Getenv(key))
	return b
}
