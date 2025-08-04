// Yoda WebSocket client: direct shell connection and file download
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   filepath.Base(os.Args[0]),
	Short: "Yoda remote client",
	CompletionOptions: cobra.CompletionOptions{
		DisableDefaultCmd: true,
	},
}

var shellCmd = &cobra.Command{
	Use:   "shell",
	Short: "Connect to remote shell",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🚀 Connecting to Yoda shell...")

		conn, err := createSecureWebSocketConnection("/shell")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		defer conn.Close()

		runShellSession(conn)
	},
}

var downloadCmd = &cobra.Command{
	Use:   "download <remote_path> <local_path>",
	Short: "Download a file from the remote server",
	Long: "Download a file from the remote server via secure WebSocket connection.\n\n" +
		"Syntax: download <remote_path> <local_path>\n\n" +
		"Examples:\n" +
		"  " + filepath.Base(os.Args[0]) + " download /etc/passwd ./passwd\n",
	Args: cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔽 Initiating file download...")

		conn, err := createSecureWebSocketConnection("/download")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		defer conn.Close()

		downloadCommand(conn, args)
	},
}

var psCmd = &cobra.Command{
	Use:   "ps [flags]",
	Short: "List processes on the remote server",
	Long: "List processes on the remote server via secure WebSocket connection.\n\n" +
		"Flags:\n" +
		"  -t, --tree    Display processes in tree format\n\n" +
		"Examples:\n" +
		"  " + filepath.Base(os.Args[0]) + " ps\n" +
		"  " + filepath.Base(os.Args[0]) + " ps -t\n",
	Run: func(cmd *cobra.Command, args []string) {
		tree, _ := cmd.Flags().GetBool("tree")

		fmt.Println("🔍 Fetching process list...")

		conn, err := createSecureWebSocketConnection("/ps")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		defer conn.Close()

		psCommand(conn, tree)
	},
}

var lsCmd = &cobra.Command{
	Use:   "ls [path...]",
	Short: "List directory contents on the remote server",
	Long: "List directory contents with detailed information (equivalent to ls -al).\n\n" +
		"Supports wildcards like *.txt, /home/*/.bashrc, etc.\n\n" +
		"Examples:\n" +
		"  " + filepath.Base(os.Args[0]) + " ls\n" +
		"  " + filepath.Base(os.Args[0]) + " ls /etc\n" +
		"  " + filepath.Base(os.Args[0]) + " ls '/var/log/*.log'\n" +
		"  " + filepath.Base(os.Args[0]) + " ls '/home/*/.bashrc'\n",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("📁 Listing files...")

		conn, err := createSecureWebSocketConnection("/ls")
		if err != nil {
			fmt.Printf("❌ %v\n", err)
			return
		}
		defer conn.Close()

		lsCommand(conn, args)
	},
}

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish]",
	Short: "Generate shell completion script",
	Long: "To load completions:\n\n" +
		"  Bash:\n" +
		"   source <(./" + filepath.Base(os.Args[0]) + " completion bash)\n\n" +
		"  Zsh:\n" +
		"   echo \"autoload -U compinit; compinit\" >> ~/.zshrc\n" +
		"   ./" + filepath.Base(os.Args[0]) + " completion zsh > \"${fpath[1]}/_" + filepath.Base(os.Args[0]) + "\"\n\n" +
		"  Fish:\n" +
		"   ./" + filepath.Base(os.Args[0]) + " completion fish | source\n",
	Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	ValidArgs: []string{"bash", "zsh", "fish"},
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			rootCmd.GenBashCompletion(cmd.OutOrStdout())
		case "zsh":
			rootCmd.GenZshCompletion(cmd.OutOrStdout())
		case "fish":
			rootCmd.GenFishCompletion(cmd.OutOrStdout(), true)
		}
	},
}

func init() {
	psCmd.Flags().BoolP("tree", "t", false, "Display processes in tree format")

	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(downloadCmd)
	rootCmd.AddCommand(psCmd)
	rootCmd.AddCommand(lsCmd)
	rootCmd.AddCommand(completionCmd)
}

func main() {
	rootCmd.Execute()
}
