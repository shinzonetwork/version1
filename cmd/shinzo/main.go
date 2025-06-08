package main

import (
	"os"
	"shinzo/version1/cli"
)

func main() {
	shinzoCmd := cli.NewShinzoCommand()
	if err := shinzoCmd.Execute(); err != nil {
		// this error is okay to discard because cobra
		// logs any errors encountered during execution
		//
		// exiting with a non-zero status code signals
		// that an error has ocurred during execution
		os.Exit(1)
	}
}
