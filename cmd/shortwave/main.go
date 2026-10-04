// Command shortwave listens to shortwave radio via public KiwiSDR receivers.
package main

import (
	"os"

	"github.com/CHE3MZ/shortwave/internal/cli"
)

func main() {
	os.Exit(cli.Run())
}
