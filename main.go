package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/cmd"
	"github.com/baalimago/go_away_boilerplate/pkg/cmd/version"
	"github.com/baalimago/go_away_boilerplate/pkg/shutdown"
	"github.com/baalimago/kinoview/cmd/classify"
	k_debug "github.com/baalimago/kinoview/cmd/debug"
	"github.com/baalimago/kinoview/cmd/serve"
)

var commands = map[string]cmd.Command{
	"s|serve":    serve.Command(),
	"c|classify": classify.Command(),
	"d|debug":    k_debug.Command(),
	"v|version":  version.Command(),
}

const usage = `== Kinoview ==

This tool categorizes your local media files, and allows for easy
inspection and view via html. Think of it as Plex, but simpler.

Target any directory, and Kinoview will crawl to find all media files
recursively and add them to an internal media database. After this, you may
either view the media files as is, or enable LLM categorization. 

Commands:
%v`

func run(args []string) int {
	ancli.Newline = true
	ancli.SetupSlog()
	setupStderrFilter()
	version.Name = "Kinoview"
	ctx, cancel := context.WithCancel(context.Background())
	exitCodeChan := make(chan int, 1)
	go func() {
		exitCodeChan <- cmd.Run(ctx, args, commands, usage)
		cancel()
	}()
	shutdown.MonitorV2(ctx, cancel)
	return <-exitCodeChan
}

func main() {
	os.Exit(run(os.Args))
}

// setupStderrFilter suppresses cosmetic "failed to enrich chat with cost
// estimate" errors from clai's cost-manager finalizer. The error writes to
// os.Stderr via ancli.PrintErr, bypassing per-worker SetOutput log files.
// A process-level pipe filter avoids the race inherent in per-goroutine
// os.Stderr redirects and runs for the entire process lifetime.
func setupStderrFilter() {
	originalStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		fmt.Fprintf(originalStderr, "failed to create stderr filter pipe: %v\n", err)
		return
	}
	os.Stderr = w

	go func() {
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "failed to enrich chat with cost estimate") {
				continue
			}
			fmt.Fprintln(originalStderr, line)
		}
	}()
}
