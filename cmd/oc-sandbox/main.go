package main

import (
	"fmt"
	"os"
)

// Version is set at build time via -ldflags "-X main.Version=<tag>".
var Version = "dev"

func main() {
	fmt.Fprintf(os.Stdout, "oc-sandbox %s\n", Version)
}
