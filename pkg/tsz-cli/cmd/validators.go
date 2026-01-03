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

var validatorsCmd = &cobra.Command{
	Use:   "validators",
	Short: "Manage format validators and guardrails",
}

var validatorsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all validators",
	RunE: func(cmd *cobra.Command, args []string) error {
		items, err := client.ListValidators(context.Background())
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(items)
	},
}

var (
	valName    string
	valType    string
	valRule    string
	valDesc    string
	valExpResp string
)

var validatorsAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new validator",
	RunE: func(cmd *cobra.Command, args []string) error {
		if valName == "" || valType == "" || valRule == "" {
			return fmt.Errorf("name, type and rule are required")
		}
		v := tszclient.FormatValidator{
			Name:             valName,
			Type:             valType,
			Rule:             valRule,
			Description:      valDesc,
			ExpectedResponse: valExpResp,
		}
		created, err := client.CreateValidator(context.Background(), v)
		if err != nil {
			return err
		}
		fmt.Println("Validator created successfully:")
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(created)
	},
}

var validatorsRemoveCmd = &cobra.Command{
	Use:   "remove [id]",
	Short: "Remove a validator by ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid ID")
		}
		if err := client.DeleteValidator(context.Background(), id); err != nil {
			return err
		}
		fmt.Printf("Validator %d deleted successfully\n", id)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(validatorsCmd)
	validatorsCmd.AddCommand(validatorsListCmd)
	validatorsCmd.AddCommand(validatorsAddCmd)
	validatorsCmd.AddCommand(validatorsRemoveCmd)

	validatorsAddCmd.Flags().StringVar(&valName, "name", "", "Validator Name")
	validatorsAddCmd.Flags().StringVar(&valType, "type", "", "Type (BUILTIN, REGEX, SCHEMA, AI_PROMPT)")
	validatorsAddCmd.Flags().StringVar(&valRule, "rule", "", "Rule content (Regex, Prompt, Schema)")
	validatorsAddCmd.Flags().StringVar(&valDesc, "desc", "", "Description")
	validatorsAddCmd.Flags().StringVar(&valExpResp, "expected", "", "Expected response (for AI_PROMPT)")
}
