package main

import (
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
	os.Exit(root.Execute(factory, os.Args[1:]))
}
