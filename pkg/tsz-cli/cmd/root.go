package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/thyrisAI/safe-zone/pkg/tszclient-go"
)

var (
	serverURL string
	apiKey    string
	client    *tszclient.Client
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "tsz",
	Short: "TSZ (Thyris Safe Zone) CLI",
	Long: `TSZ CLI allows you to interact with the Safe Zone Gateway
to detect PII, manage patterns, allowlists, blocklists, and more.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		var err error
		cfg := tszclient.Config{
			BaseURL: serverURL,
			APIKey:  apiKey,
		}
		client, err = tszclient.New(cfg)
		return err
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&serverURL, "url", "http://localhost:8080", "TSZ Server URL")
	rootCmd.PersistentFlags().StringVar(&apiKey, "key", "", "Admin API Key (required for management commands)")
}
