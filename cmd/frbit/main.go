package main

import (
	"fmt"
	"os"

	"github.com/fortrabbit/frbit-cli/internal/app"
	"github.com/fortrabbit/frbit-cli/internal/cmd/root"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	factory := app.NewFactory(version, commit, date)
	command := root.NewCmdRoot(factory)
	if err := command.Execute(); err != nil {
		fmt.Fprintln(factory.IOStreams.ErrOut, "error:", err)
		os.Exit(1)
	}
}
