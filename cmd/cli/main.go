// Yoda WebSocket client: direct shell connection
package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "yoda",
	Short: "Yoda remote shell client",
	Long: `Yoda is a secure WebSocket-based remote shell client.
It connects to a Yoda server over mTLS WebSocket to provide 
interactive shell access with advanced features.`,
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Connect to remote shell",
	Long: `Connect to the remote Yoda server and start an interactive shell session.
This command establishes a secure WebSocket connection over mTLS.`,
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚀 Connecting to Yoda shell...")
		runShellSession()
	},
}

func init() {
	rootCmd.AddCommand(shellCmd)
}

func main() {
	rootCmd.Execute()
}
