package commands

import (
	"fmt"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/logger"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/repo_sync"
	"github.com/spf13/cobra"
)

// CheckoutOptions holds the options for the checkout command
// 优化参数结构体，增加与 start/branch 命令一致的参数
// 支持 --all, --jobs, --quiet, --verbose
// 支持 --branch 指定分支名
// 支持 --detach, --force-sync, --force-overwrite
// 支持 Manifest 相关参数
type CheckoutOptions struct {
	Detach         bool
	ForceSync      bool
	ForceOverwrite bool
	JobsCheckout   int
	Quiet          bool
	Verbose        bool
	All            bool
	Branch         string
	DefaultRemote  string
	Config         *config.Config
	CommonManifestOptions
}

// CheckoutCmd creates the checkout command
func CheckoutCmd() *cobra.Command {
	opts := &CheckoutOptions{}
	cmd := &cobra.Command{
		Use:   "checkout <branch> [<project>...]",
		Short: "Checkout a branch for development",
		Long:  `Checks out a branch for development, creating it if necessary.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			return runCheckout(opts, args)
		},
	}
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force overwrite existing files")
	cmd.Flags().IntVarP(&opts.JobsCheckout, "jobs", "j", 8, "number of projects to checkout in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVar(&opts.All, "all", false, "checkout branch in all projects")
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "specify an alternate branch name")
	cmd.Flags().StringVar(&opts.DefaultRemote, "default-remote", "", "specify the default remote name for checkout")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}

// runCheckout executes the checkout command logic
func runCheckout(opts *CheckoutOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	if len(args) < 1 {
		return fmt.Errorf("missing branch name")
	}
	branchName := args[0]
	if opts.Branch != "" {
		branchName = opts.Branch
	}
	projectNames := args[1:]
	cfg := opts.Config

	log.Info("正在检出分支 '%s'", branchName)

	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	manager := project.NewManagerFromManifest(manifestObj, cfg)
	var projects []*project.Project
	if opts.All || len(projectNames) == 0 {
		log.Debug("获取所有项目")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取项目列表失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		log.Debug("获取指定项目: %v", projectNames)
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("获取指定项目失败: %v", err)
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	log.Info("开始检出 %d 个项目", len(projects))

	// 使用 repo_sync 包中的 Engine 进行检出操作
	syncOpts := &repo_sync.Options{
		Detach:         opts.Detach,
		ForceSync:      opts.ForceSync,
		ForceOverwrite: opts.ForceOverwrite,
		JobsCheckout:   opts.JobsCheckout,
		Quiet:          opts.Quiet,
		Verbose:        opts.Verbose,
		DefaultRemote:  opts.DefaultRemote, // 添加DefaultRemote参数
	}

	engine := repo_sync.NewEngine(syncOpts, nil, log)
	// 设置分支名称
	engine.SetBranchName(branchName)
	// 执行检出操作
	err = engine.CheckoutBranch(projects)
	if err != nil {
		log.Error("检出分支失败: %v", err)
		return err
	}

	// 获取检出结果
	success, failed := engine.GetCheckoutStats()

	if !opts.Quiet {
		log.Info("检出分支 '%s' 完成: %d 成功, %d 失败", branchName, success, failed)
	}

	if failed > 0 {
		return fmt.Errorf("checkout failed for %d projects", failed)
	}

	return nil
}
