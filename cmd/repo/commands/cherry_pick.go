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

// CherryPickOptions holds the options for the cherry-pick command
type CherryPickOptions struct {
	All     bool
	Jobs    int
	Quiet   bool
	Verbose bool
	Config  *config.Config
	CommonManifestOptions
}

// CherryPickCmd creates the cherry-pick command
func CherryPickCmd() *cobra.Command {
	opts := &CherryPickOptions{}
	cmd := &cobra.Command{
		Use:   "cherry-pick <commit> [<project>...]",
		Short: "Cherry-pick a commit onto the current branch",
		Long:  `Applies the changes introduced by the named commit(s) onto the current branch.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			return runCherryPick(opts, args)
		},
	}
	cmd.Flags().BoolVar(&opts.All, "all", false, "cherry-pick in all projects")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of projects to cherry-pick in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}

// runCherryPick executes the cherry-pick command logic
func runCherryPick(opts *CherryPickOptions, args []string) error {
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
		return fmt.Errorf("missing commit hash")
	}
	commit := args[0]
	projectNames := args[1:]
	cfg := opts.Config

	log.Info("正在应用 cherry-pick '%s'", commit)

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

	log.Info("开始在 %d 个项目中应用 cherry-pick", len(projects))

	// 使用 repo_sync 包中Engine 进行 cherry-pick 操作
	syncOpts := &repo_sync.Options{
		Jobs:    opts.Jobs,
		Quiet:   opts.Quiet,
		Verbose: opts.Verbose,
	}

	engine := repo_sync.NewEngine(syncOpts, nil, log)
	// 设置提交哈希
	engine.SetCommitHash(commit)
	// 执行 cherry-pick 操作
	err = engine.CherryPickCommit(projects)
	if err != nil {
		log.Error("Cherry-pick 失败: %v", err)
		return err
	}

	// 获取 cherry-pick 结果
	success, failed := engine.GetCherryPickStats()

	if !opts.Quiet {
		log.Info("Cherry-pick 提交 '%s' 完成: %d 成功, %d 失败", commit, success, failed)
	}

	if failed > 0 {
		return fmt.Errorf("cherry-pick failed for %d projects", failed)
	}

	return nil
}
