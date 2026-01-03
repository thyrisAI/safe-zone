package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

var (
	scanText string
	scanFile string
	scanRID  string
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan text or file for PII",
	RunE: func(cmd *cobra.Command, args []string) error {
		var text string
		if scanFile != "" {
			b, err := os.ReadFile(scanFile)
			if err != nil {
				return fmt.Errorf("failed to read file: %w", err)
			}
			text = string(b)
		} else if scanText != "" {
			text = scanText
		} else {
			return fmt.Errorf("either --text or --file must be provided")
		}

		ctx := context.Background()
		opts := []tszclient.DetectOption{}
		if scanRID != "" {
			opts = append(opts, tszclient.WithRID(scanRID))
		}

		resp, err := client.DetectText(ctx, text, opts...)
		if err != nil {
			return fmt.Errorf("detection failed: %w", err)
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(resp)
	},
}

func init() {
	rootCmd.AddCommand(scanCmd)
	scanCmd.Flags().StringVarP(&scanText, "text", "t", "", "Text content to scan")
	scanCmd.Flags().StringVarP(&scanFile, "file", "f", "", "File path to scan")
	scanCmd.Flags().StringVar(&scanRID, "rid", "", "Request ID for audit logs")
}
