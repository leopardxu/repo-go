package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// StartOptions 包含start命令的选项
type StartOptions struct {
	All              bool
	Rev             string
	Branch          string
	Jobs           int
	Verbose        bool
	Quiet          bool
	OuterManifest  bool
	NoOuterManifest bool
	ThisManifestOnly bool
	HEAD           bool
}

// StartCmd 返回start命令
func StartCmd() *cobra.Command {
	opts := &StartOptions{}

	cmd := &cobra.Command{
		Use:   "start <branch_name> [<project>...]]",
		Short: "Start a new branch for development",
		Long:  `Create a new branch for development based on the current manifest.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVar(&opts.All, "all", false, "start branch in all projects")
	cmd.Flags().StringVarP(&opts.Rev, "rev", "r", "", "start branch from the specified revision")
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "specify an alternate branch name")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	manifestOpts := &CommonManifestOptions{
		OuterManifest:    opts.OuterManifest,
		NoOuterManifest:  opts.NoOuterManifest,
		ThisManifestOnly: opts.ThisManifestOnly,
	}
	AddManifestFlags(cmd, manifestOpts)
	cmd.Flags().BoolVar(&opts.HEAD, "HEAD", false, "abbreviation for --rev HEAD")

	return cmd
}

// runStart 执行start命令
func runStart(opts *StartOptions, args []string) error {
	if opts.HEAD {
		opts.Rev = "HEAD"
	}
	// 获取分支名称
	branchName := args[0]
	if opts.Branch != "" {
		branchName = opts.Branch
	}

	// 获取项目列表
	projectNames := args[1:]

	fmt.Printf("Starting branch '%s'\n", branchName)

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg)

	// 获取要处理的项目
	var projects []*project.Project
	if opts.All || len(projectNames) == 0 {
		// 如果指定了--all或没有指定项目，则处理所有项目
		projects, err = manager.GetProjects("")
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 在每个项目中创建分支
	for _, p := range projects {
		fmt.Printf("Creating branch '%s' in project '%s'\n", branchName, p.Name)
		
		// 创建分支
		revision := opts.Rev
		if revision == "" {
			revision = p.Revision
		}
		
		if err := p.GitRepo.CreateBranch(branchName, revision); err != nil {
			return fmt.Errorf("failed to create branch in project %s: %w", p.Name, err)
		}
	}

	fmt.Printf("Branch '%s' created successfully\n", branchName)
	return nil
}