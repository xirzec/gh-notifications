package main

import (
	"fmt"
	"os"
)

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gh-notifications: %v\n", err)
		os.Exit(1)
	}

	if err := runNotifications(opts); err != nil {
		fmt.Fprintf(os.Stderr, "gh-notifications: %v\n", err)
		os.Exit(1)
	}
}
