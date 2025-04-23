package commands

import (
	"fmt"
	"runtime"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/repo_sync"
	"github.com/spf13/cobra"
)

// AbandonOptions 包含abandon命令的选项
type AbandonOptions struct {
	CommonManifestOptions
	Project string
	DryRun  bool
	All     bool   // 删除所有分支
	Jobs    int    // 并行任务数
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
			fmt.Printf("Would abandon branch '%s'\n", branchName)
		} else {
			fmt.Printf("Abandoning branch '%s'\n", branchName)
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	manager := project.NewManager(manifestObj, cfg)

	var projects []*project.Project
	if len(projectNames) == 0 {
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			return fmt.Errorf("failed to get projects by names: %w", err)
		}
	}

	engine := repo_sync.NewEngine(nil, &repo_sync.Options{JobsCheckout: opts.Jobs, Quiet: opts.Quiet}, nil, nil)
	results := engine.AbandonTopics(projects, branchName)
	repo_sync.PrintAbandonSummary(results)
	return nil
}