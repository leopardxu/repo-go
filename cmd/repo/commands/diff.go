package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DiffOptions 包含diff命令的选项
type DiffOptions struct {
	Unified         int
	Cached          bool
	NameOnly        bool
	NameStatus      bool
	Stat            bool
	Absolute        bool
	Jobs            int
	Verbose         bool
	Quiet           bool
	OuterManifest   bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// DiffCmd 返回diff命令
func DiffCmd() *cobra.Command {
	opts := &DiffOptions{}

	cmd := &cobra.Command{
		Use:   "diff [<project>...]",
		Short: "Show changes between commits, commit and working tree, etc",
		Long:  `Show changes between commits, commit and working tree, etc.

The -u option causes 'repo diff' to generate diff output with file paths
relative to the repository root, so the output can be applied
to the Unix 'patch' command.

By default, 'repo diff' shows changes that are not staged for commit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Unified, "unified", "u", 3, "generate diffs with <n> lines of context")
	cmd.Flags().BoolVarP(&opts.Cached, "cached", "c", false, "show staged changes")
	cmd.Flags().BoolVar(&opts.NameOnly, "name-only", false, "show only names of changed files")
	cmd.Flags().BoolVar(&opts.NameStatus, "name-status", false, "show only names and status of changed files")
	cmd.Flags().BoolVar(&opts.Stat, "stat", false, "show diffstat instead of patch")
	cmd.Flags().BoolVarP(&opts.Absolute, "absolute", "a", false, "paths are relative to the repository root")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel (default: 8; based on number of CPU cores)")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runDiff 执行diff命令
func runDiff(opts *DiffOptions, args []string) error {
	fmt.Println("Showing differences in projects")

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
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects("")
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 构建diff命令选项
	diffArgs := []string{"diff"}
	
	if opts.Unified > 0 {
		diffArgs = append(diffArgs, fmt.Sprintf("-U%d", opts.Unified))
	}
	
	if opts.Cached {
		diffArgs = append(diffArgs, "--cached")
	}
	
	if opts.NameOnly {
		diffArgs = append(diffArgs, "--name-only")
	}
	
	if opts.NameStatus {
		diffArgs = append(diffArgs, "--name-status")
	}
	
	if opts.Stat {
		diffArgs = append(diffArgs, "--stat")
	}

	// 对每个项目执行diff
	for _, p := range projects {
		fmt.Printf("\nProject %s:\n", p.Name)
		
		// 执行diff命令
		output, err := p.GitRepo.RunCommand(diffArgs...)
		if err != nil {
			// 忽略错误，因为git diff在有差异时会返回非零退出码
			// 但我们仍然想显示输出
		}
		
		if output != "" {
			fmt.Println(output)
		} else {
			fmt.Println("No changes")
		}
	}

	return nil
}