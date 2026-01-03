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

var blocklistCmd = &cobra.Command{
	Use:   "blocklist",
	Short: "Manage blocklist items",
}

var blocklistListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all blocklist items",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := client.ListBlocklist(context.Background())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	},
}

var (
	blockValue string
	blockDesc  string
)

var blocklistAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new blocklist item",
	RunE: func(cmd *cobra.Command, args []string) error {
		if blockValue == "" {
			return fmt.Errorf("value is required")
		}
		item := tszclient.BlacklistItem{
			Value:       blockValue,
			Description: blockDesc,
		}
		created, err := client.CreateBlocklistItem(context.Background(), item)
		if err != nil {
			return err
		}
		fmt.Println("Blocklist item created successfully:")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(created)
	},
}

var blocklistRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a blocklist item by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid ID")
		}
		if err := client.DeleteBlocklistItem(context.Background(), id); err != nil {
			return err
		}
		fmt.Printf("Blocklist item %d deleted successfully\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(blocklistCmd)
	blocklistCmd.AddCommand(blocklistListCmd)
	blocklistCmd.AddCommand(blocklistAddCmd)
	blocklistCmd.AddCommand(blocklistRemoveCmd)

	blocklistAddCmd.Flags().StringVar(&blockValue, "value", "", "Value to block")
	blocklistAddCmd.Flags().StringVar(&blockDesc, "desc", "", "Description")
}
