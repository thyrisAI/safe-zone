package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

var templatesCmd = &cobra.Command{
	Use:   "templates",
	Short: "Manage guardrail templates",
}

var templateFile string

var templatesImportCmd = &cobra.Command{
	Use:   "import",
	Short: "Import a template from JSON file",
	RunE: func(cmd *cobra.Command, args []string) error {
		if templateFile == "" {
			return fmt.Errorf("--file is required")
		}

		b, err := os.ReadFile(templateFile)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}

		// Try unmarshalling into Request wrapper first
		var req tszclient.TemplateImportRequest
		if err := json.Unmarshal(b, &req); err == nil && req.Template.Name != "" {
			return client.ImportTemplate(context.Background(), req.Template)
		}

		// Try unmarshalling directly into TemplateDefinition
		var def tszclient.TemplateDefinition
		if err := json.Unmarshal(b, &def); err != nil {
			return fmt.Errorf("invalid template JSON: %w", err)
		}

		if def.Name == "" {
			// Basic validation
			return fmt.Errorf("invalid template: name is missing")
		}

		return client.ImportTemplate(context.Background(), def)
	},
}

func init() {
	rootCmd.AddCommand(templatesCmd)
	templatesCmd.AddCommand(templatesImportCmd)
	templatesImportCmd.Flags().StringVarP(&templateFile, "file", "f", "", "Template JSON file path")
}
