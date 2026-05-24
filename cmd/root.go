// Package cmd defines the CLI commands for sfc-cli.
package cmd

import (
	"github.com/spf13/cobra"
)

// NewRootCommand constructs and returns the root cobra command with
// persistent global flags for authentication and service configuration.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:          "sfc-cli",
		Short:        "CLI client for Salted Fish Cloud",
		SilenceUsage: true,
		SilenceErrors: true,
	}

	// Global flags available to all sub-commands.
	root.PersistentFlags().String("api-ticket", "", "API ticket for authentication")
	root.PersistentFlags().String("service-url", "", "Base URL of the Salted Fish Cloud service")

	return root
}

// Execute runs the root command and returns any error encountered.
func Execute() error {
	return NewRootCommand().Execute()
}
