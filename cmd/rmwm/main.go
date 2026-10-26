package main

import (
	"os"
	"runtime/debug"
)

// ************************************************************************************************
// main runs the CLI and forwards its exit status to the operating system.
func main() {
	debug.SetPanicOnFault(true)
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
