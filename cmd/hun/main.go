package main

import (
	"os"

	"hun-control/internal/command"
)

func main() {
	os.Exit(command.NewCLI().Run(os.Args[1:], os.Stdout, os.Stderr))
}
