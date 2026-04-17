// tale-cli — administrative CLI for TalePanel.
//
// All subcommands connect to the same Postgres as the API via DATABASE_URL.
// Run from inside the API Docker image: `docker compose run --rm api tale-cli ...`.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "tale-cli",
	Short: "TalePanel administrative CLI",
}

func main() {
	rootCmd.AddCommand(adminCmd())
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
