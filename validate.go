package main

import (
	"github.com/spf13/cobra"
)

// validateCmd represents the validate command
var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "validate resources",
	Long:  `Validate resources`,
}

func init() {
	rootCommand.AddCommand(validateCmd)
}
