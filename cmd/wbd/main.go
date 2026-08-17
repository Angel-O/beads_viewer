package main

import (
	"fmt"
	"os"
)

func main() {
	app, err := newApp(os.Stdin, os.Stdout, os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wbd: %v\n", err)
		os.Exit(1)
	}
	os.Exit(app.run(os.Args[1:]))
}
