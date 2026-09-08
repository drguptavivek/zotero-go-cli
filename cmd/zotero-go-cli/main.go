package main

import (
	"fmt"
	"os"

	"github.com/Epistemic-Technology/zotero/internal/cli"
)

func main() {
	cmd := cli.NewCommand()
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
