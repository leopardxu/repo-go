package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/leopardxu/repo-go/internal/repo_sync"
	"github.com/spf13/cobra"
)

// AbandonOptions 包含abandon命令的选项
type AbandonOptions struct {
	CommonManifestOptions
	Project string
	DryRun  bool
	All     bool   // 删除所有分�?
	Jobs    int    // 并行任务�?
	Verbose bool   // 详细输出
	Quiet   bool   // 静默模式
	Force   bool   // 强制删除
	Keep   bool    // 保留分支引用
}

func AbandonCmd() *cobra.Command {
	opts := &AbandonOptions{}

	cmd := &cobra.Command{
		Use:   "abandon [--all | <branchname>] [<project>...]",
		Short: "Permanently abandon a development branch",
		Long: `This subcommand permanently abandons a development branch by
deleting it (and all its history) from your local repository.

It is equivalent to "git branch -D <branchname>".`,
		Args: cobra.MinimumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAbandon(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVar(&opts.All, "all", false, "delete all branches in all projects")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", runtime.NumCPU() * 2, "number of jobs to run in parallel (default: based on CPU cores)")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "force deletion of branch")
	cmd.Flags().BoolVar(&opts.Keep, "keep", false, "keep branch reference")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "only show what would be done")
	
	// 添加多清单选项
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runAbandon 执行abandon命令
func runAbandon(opts *AbandonOptions, args []string) error {
	// 初始化日志系�?
	log := logger.NewDefaultLogger()
	if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	branchName := ""
	if len(args) > 0 {
		branchName = args[0]
	}
	projectNames := []string{}
	if len(args) > 1 {
		projectNames = args[1:]
	}
	if opts.Project != "" {
		projectNames = append(projectNames, opts.Project)
	}

	if !opts.Quiet {
		if opts.DryRun {
			log.Info("将要放弃分支 '%s'", branchName)
		} else {
			log.Info("正在放弃分支 '%s'", branchName)
		}
	}

	log.Debug("正在加载配置文件...")
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置文件失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	
	log.Debug("正在解析清单文件 %s...", cfg.ManifestName)
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups,","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifestObj, cfg)

	var projects []*project.Project
	if len(projectNames) == 0 {
		log.Debug("获取所有项�?..")
		projects, err = manager.GetProjectsInGroups(nil)
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	} else {
		log.Debug("根据名称获取项目: %v", projectNames)
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("failed to get projects by names: %w", err)
		}
		log.Debug("共获取到 %d 个项�?, len(projects))
	}

	// 创建引擎并设置选项
	log.Debug("创建同步引擎，并行任务数: %d", opts.Jobs)
	engine := repo_sync.NewEngine(&repo_sync.Options{
		JobsCheckout: opts.Jobs, 
		Quiet: opts.Quiet,
		Verbose: opts.Verbose,
		DryRun: opts.DryRun,
		Force: opts.Force,
	}, manifestObj, log)
	
	// 执行放弃分支操作
	if !opts.Quiet {
		log.Info("开始处�?%d 个项目的分支放弃操作...", len(projects))
	}
	results := engine.AbandonTopics(projects, branchName)
	
	// 输出结果汇�?
	repo_sync.PrintAbandonSummary(results, log)
	return nil
}
