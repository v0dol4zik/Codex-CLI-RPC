package main

import (
	"os"

	"github.com/v0dol4zik/Codex-CLI-RPC/internal/app"
)

func main() {
	os.Exit((app.App{Stdout: os.Stdout, Stderr: os.Stderr}).Run(os.Args[1:]))
}
