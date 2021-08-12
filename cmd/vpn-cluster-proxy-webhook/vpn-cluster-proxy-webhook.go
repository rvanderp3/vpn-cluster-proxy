package main

import (
	util "github.com/rvanderp/ci-ibm-cloud-webhook/pkg/util"
	"github.com/spf13/cobra"
	"os"
	"path/filepath"
)

func newRootCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           filepath.Base(os.Args[0]),
		Short:         "Mutating Webhook HTTPS_PROXY injection",
		Long:          "",
		Run:           runRootCmd,
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	return cmd
}

func runRootCmd(cmd *cobra.Command, args []string) {
	util.SetupWebhookListener()
}

func main() {
	rootCmd := newRootCmd()
	rootCmd.Execute()
}
