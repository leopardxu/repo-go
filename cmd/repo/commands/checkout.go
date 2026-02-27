package commands

import (
	"fmt"
	"strings"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/leopardxu/repo-go/internal/repo_sync"
	"github.com/spf13/cobra"
)

// CheckoutOptions holds the options for the checkout command
// 简化参数结构体，与原生git-repo保持一致
type CheckoutOptions struct {
	JobsCheckout int
	Quiet        bool
	Verbose      bool
	Config       *config.Config
	CommonManifestOptions
}

// CheckoutCmd creates the checkout command
func CheckoutCmd() *cobra.Command {
	opts := &CheckoutOptions{}
	cmd := &cobra.Command{
		Use:   "checkout <branchname> [<project>...]",
		Short: "Checkout a branch for development",
		Long:  `Checks out an existing branch that was previously created by 'repo start'.`,
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
	cmd.Flags().IntVarP(&opts.JobsCheckout, "jobs", "j", 8, "number of projects to checkout in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
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

	// 确保在repo根目录下执行
	originalDir, err := EnsureRepoRoot(log)
	if err != nil {
		log.Error("查找repo根目录失败: %v", err)
		return fmt.Errorf("failed to locate repo root: %w", err)
	}
	defer RestoreWorkDir(originalDir, log)

	if len(args) < 1 {
		return fmt.Errorf("missing branch name")
	}
	branchName := args[0]
	projectNames := args[1:]
	cfg := opts.Config

	log.Info("正在检出分'%s'", branchName)

	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	manager := project.NewManagerFromManifest(manifestObj, cfg)
	var projects []*project.Project
	if len(projectNames) == 0 {
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
		JobsCheckout: opts.JobsCheckout,
		Quiet:        opts.Quiet,
		Verbose:      opts.Verbose,
	}

	engine := repo_sync.NewEngine(syncOpts, nil, log)
	// 设置分支名称
	engine.SetBranchName(branchName)
	// 执行检出操作
	err = engine.CheckoutBranch(projects)
	if err != nil {
		log.Error("检出分支失 %v", err)
		return err
	}

	// 获取检出结
	success, failed := engine.GetCheckoutStats()

	if !opts.Quiet {
		log.Info("检出分'%s' 完成: %d 成功, %d 失败", branchName, success, failed)
	}

	if failed > 0 {
		return fmt.Errorf("checkout failed for %d projects", failed)
	}

	return nil
}
