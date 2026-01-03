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

var allowlistCmd = &cobra.Command{
	Use:   "allowlist",
	Short: "Manage allowlist items",
}

var allowlistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all allowlist items",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := client.ListAllowlist(context.Background())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	},
}

var (
	allowValue string
	allowDesc  string
)

var allowlistAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new allowlist item",
	RunE: func(cmd *cobra.Command, args []string) error {
		if allowValue == "" {
			return fmt.Errorf("value is required")
		}
		item := tszclient.AllowlistItem{
			Value:       allowValue,
			Description: allowDesc,
		}
		created, err := client.CreateAllowlistItem(context.Background(), item)
		if err != nil {
			return err
		}
		fmt.Println("Allowlist item created successfully:")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(created)
	},
}

var allowlistRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove an allowlist item by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid ID")
		}
		if err := client.DeleteAllowlistItem(context.Background(), id); err != nil {
			return err
		}
		fmt.Printf("Allowlist item %d deleted successfully\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(allowlistCmd)
	allowlistCmd.AddCommand(allowlistListCmd)
	allowlistCmd.AddCommand(allowlistAddCmd)
	allowlistCmd.AddCommand(allowlistRemoveCmd)

	allowlistAddCmd.Flags().StringVar(&allowValue, "value", "", "Value to allow")
	allowlistAddCmd.Flags().StringVar(&allowDesc, "desc", "", "Description")
}
