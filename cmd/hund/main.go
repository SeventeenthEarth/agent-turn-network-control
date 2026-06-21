package main

import (
	"os"

	"hun-control/internal/command"
)

func main() {
	os.Exit(command.NewDaemon().Run(os.Args[1:], os.Stdout, os.Stderr))
}
