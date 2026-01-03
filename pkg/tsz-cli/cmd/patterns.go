package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/spf13/cobra"
	"github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

var patternsCmd = &cobra.Command{
	Use:   "patterns",
	Short: "Manage detection patterns",
}

var patternsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all patterns",
	RunE: func(cmd *cobra.Command, args []string) error {
		patterns, err := client.ListPatterns(context.Background())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(patterns)
	},
}

var (
	patName           string
	patRegex          string
	patDesc           string
	patCategory       string
	patBlockThreshold float64
	patAllowThreshold float64
)

var patternsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new pattern",
	RunE: func(cmd *cobra.Command, args []string) error {
		if patName == "" || patRegex == "" {
			return fmt.Errorf("name and regex are required")
		}
		p := tszclient.Pattern{
			Name:           patName,
			Regex:          patRegex,
			Description:    patDesc,
			Category:       patCategory,
			IsActive:       true,
			BlockThreshold: patBlockThreshold,
			AllowThreshold: patAllowThreshold,
		}
		created, err := client.CreatePattern(context.Background(), p)
		if err != nil {
			return err
		}
		fmt.Println("Pattern created successfully:")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(created)
	},
}

var patternsRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a pattern by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid ID")
		}
		if err := client.DeletePattern(context.Background(), id); err != nil {
			return err
		}
		fmt.Printf("Pattern %d deleted successfully\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(patternsCmd)
	patternsCmd.AddCommand(patternsListCmd)
	patternsCmd.AddCommand(patternsAddCmd)
	patternsCmd.AddCommand(patternsRemoveCmd)

	patternsAddCmd.Flags().StringVar(&patName, "name", "", "Pattern Name")
	patternsAddCmd.Flags().StringVar(&patRegex, "regex", "", "Pattern Regex")
	patternsAddCmd.Flags().StringVar(&patDesc, "desc", "", "Description")
	patternsAddCmd.Flags().StringVar(&patCategory, "category", "PII", "Category (PII, SECRET, etc)")
	patternsAddCmd.Flags().Float64Var(&patBlockThreshold, "block-threshold", 0.0, "Block threshold override")
	patternsAddCmd.Flags().Float64Var(&patAllowThreshold, "allow-threshold", 0.0, "Allow threshold override")
}
