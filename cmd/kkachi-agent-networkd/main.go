package main

import (
	"os"

	"kkachi-agent-network-control/internal/command"
)

func main() {
	os.Exit(command.NewDaemon().Run(os.Args[1:], os.Stdout, os.Stderr))
}
