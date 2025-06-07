package main

import (
	"fmt"
	"os"

	"github.com/leopardxu/repo-go/cmd/repo/commands"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/spf13/cobra"
)

var (
	// 版本信息，将在构建时注入
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// 初始化日志
	log := logger.NewDefaultLogger()
	logFile := os.Getenv("GOGO_LOG_FILE")
	if logFile != "" {
		if err := log.SetDebugFile(logFile); err != nil {
			fmt.Printf("警告: 无法设置日志文件 %s: %v\n", logFile, err)
		}
	}
	logger.SetGlobalLogger(log)

	// 创建根命令
	rootCmd := &cobra.Command{
		Use:   "repo [-p|--paginate|--no-pager] COMMAND [ARGS]",
		Short: "Repo is a tool for managing multiple git repositories",
		Long: `Usage: repo [-p|--paginate|--no-pager] COMMAND [ARGS]

Options:
  -h, --help            show this help message and exit
  --help-all            show this help message with all subcommands and exit
  -p, --paginate        display command output in the pager
  --no-pager            disable the pager
  --color=COLOR         control color usage: auto, always, never
  --trace               trace git command execution (REPO_TRACE=1)
  --trace-go            trace go command execution
  --time                time repo command execution
  --version             display this version of repo
  --show-toplevel       display the path of the top-level directory of the repo client checkout
  --event-log=EVENT_LOG filename of event log to append timeline to
  --git-trace2-event-log=GIT_TRACE2_EVENT_LOG directory to write git trace2 event log to
  --submanifest-path=REL_PATH submanifest path
        `,
		Version: fmt.Sprintf("%s (commit: %s, built at: %s)",
			version, commit, date),
	}

	// 设置PersistentPreRun钩子函数处理全局标志
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		// 处理--trace标志
		trace, _ := cmd.Flags().GetBool("trace")
		if trace {
			// 设置REPO_TRACE环境变量
			os.Setenv("REPO_TRACE", "1")
			// 设置日志级别为Trace
			logger.SetLevel(logger.LogLevelTrace)
			log.Trace("启用了跟踪模式")
		}

		// 处理--trace-go标志
		traceGo, _ := cmd.Flags().GetBool("trace-go")
		if traceGo {
			// 设置Go运行时跟踪
			os.Setenv("GODEBUG", "http2debug=2,gctrace=1")
			log.Trace("启用了Go运行时跟踪")
		}

		// 处理--color标志
		color, _ := cmd.Flags().GetBool("color")
		if !color {
			// 禁用颜色输出
			os.Setenv("NO_COLOR", "1")
		}

		// 处理--git-trace2-event-log标志
		eventLog, _ := cmd.Flags().GetString("git-trace2-event-log")
		if eventLog != "" {
			// 设置Git Trace2事件日志
			os.Setenv("GIT_TRACE2_EVENT", eventLog)
			log.Trace("Git Trace2事件日志已设置为: %s", eventLog)
		}
	}

	// 全局选项
	rootCmd.PersistentFlags().Bool("paginate", false, "display command output in the pager")
	rootCmd.PersistentFlags().Bool("no-pager", false, "disable the pager")
	rootCmd.PersistentFlags().String("color", "auto", "control color usage: auto, always, never")
	rootCmd.PersistentFlags().Bool("trace", false, "trace git command execution (REPO_TRACE=1)")
	rootCmd.PersistentFlags().Bool("trace-go", false, "trace go command execution")
	rootCmd.PersistentFlags().Bool("time", false, "time repo command execution")
	rootCmd.PersistentFlags().Bool("show-toplevel", false, "display the path of the top-level directory of the repo client checkout")
	rootCmd.PersistentFlags().String("event-log", "", "filename of event log to append timeline to")
	rootCmd.PersistentFlags().String("git-trace2-event-log", "", "directory to write git trace2 event log to")
	rootCmd.PersistentFlags().String("submanifest-path", "", "submanifest path")

	// 添加子命令
	rootCmd.AddCommand(commands.InitCmd())
	rootCmd.AddCommand(commands.SyncCmd())
	rootCmd.AddCommand(commands.StartCmd())
	rootCmd.AddCommand(commands.StatusCmd())
	rootCmd.AddCommand(commands.DiffCmd())
	rootCmd.AddCommand(commands.UploadCmd())
	rootCmd.AddCommand(commands.ForallCmd())
	rootCmd.AddCommand(commands.ManifestCmd())
	rootCmd.AddCommand(commands.PruneCmd())
	rootCmd.AddCommand(commands.AbandonCmd())
	rootCmd.AddCommand(commands.BranchCmd())
	rootCmd.AddCommand(commands.CheckoutCmd())
	rootCmd.AddCommand(commands.CherryPickCmd())
	rootCmd.AddCommand(commands.DownloadCmd())
	rootCmd.AddCommand(commands.GrepCmd())
	rootCmd.AddCommand(commands.InfoCmd())
	rootCmd.AddCommand(commands.ListCmd())
	rootCmd.AddCommand(commands.RebaseCmd())
	rootCmd.AddCommand(commands.SmartSyncCmd())
	rootCmd.AddCommand(commands.StageCmd())

	// 执行命令
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
