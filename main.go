package main

import (
	"context"
	"os"

	"github.com/baalimago/go_away_boilerplate/pkg/ancli"
	"github.com/baalimago/go_away_boilerplate/pkg/shutdown"
	"github.com/baalimago/kinoview/cmd/serve"
	"github.com/baalimago/wd-41/cmd"
	"github.com/baalimago/wd-41/cmd/version"
)

var commands = map[string]cmd.Command{
	"s|serve":   serve.Command(),
	"v|version": version.Command(),
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
