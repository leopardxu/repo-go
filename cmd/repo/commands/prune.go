package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/git"
	"github.com/spf13/cobra"
)

// PruneOptions 包含prune命令的选项
type PruneOptions struct {
	Force            bool
	DryRun           bool
	Verbose          bool
	Quiet            bool
	Jobs             int
	OuterManifest    bool
	NoOuterManifest  bool
	ThisManifestOnly bool
}

// PruneCmd 返回prune命令
func PruneCmd() *cobra.Command {
	opts := &PruneOptions{}

	cmd := &cobra.Command{
		Use:   "prune [<project>...]",
		Short: "Prune (delete) already merged topics",
		Long:  `Prune (delete) already merged topics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrune(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "force pruning even if there are local changes")
	cmd.Flags().BoolVarP(&opts.DryRun, "dry-run", "n", false, "don't actually prune, just show what would be pruned")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runPrune 执行prune命令
// runPrune executes the prune command logic
func runPrune(opts *PruneOptions, args []string) error {
	fmt.Println("Pruning projects not in manifest")

	// 加载配置
	cfg, err := config.Load() // Declare err here
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName) // Reuse err
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg) // Assuming manifest and cfg are loaded

	var projects []*project.Project
	// err is already declared

	if len(args) == 0 {
		projects, err = manager.GetProjects(nil) // Use =
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(args) // Use =
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	// 创建项目路径映射
	projectPaths := make(map[string]bool)
	for _, p := range projects {
		projectPaths[p.Path] = true
	}

	// 获取工作目录中的所有目录
	workDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get working directory: %w", err)
	}

	entries, err := os.ReadDir(workDir)
	if err != nil {
		return fmt.Errorf("failed to read working directory: %w", err)
	}

	// 查找不在清单中的项目
	var prunedProjects []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// 跳过.repo目录
		if entry.Name() == ".repo" {
			continue
		}

		// 检查目录是否是Git仓库
		gitDir := filepath.Join(workDir, entry.Name(), ".git")
		if _, err := os.Stat(gitDir); os.IsNotExist(err) {
			continue
		}

		// 检查目录是否在清单中
		if !projectPaths[entry.Name()] {
			prunedProjects = append(prunedProjects, entry.Name())
		}
	}

	// 如果没有要删除的项目，直接返回
	if len(prunedProjects) == 0 {
		fmt.Println("No projects to prune")
		return nil
	}

	// 显示要删除的项目
	fmt.Printf("Found %d projects to prune:\n", len(prunedProjects))
	for _, name := range prunedProjects {
		fmt.Printf("  %s\n", name)
	}

	// 如果是模拟运行，直接返回
	if opts.DryRun {
		fmt.Println("Dry run, no projects were pruned")
		return nil
	}

	// 删除项目
	for _, name := range prunedProjects {
		projectPath := filepath.Join(workDir, name)
		
		// 如果启用了详细模式，显示更多信息
		if opts.Verbose {
			fmt.Printf("Pruning project %s...\n", name)
		}
		
		// 如果不是强制模式，检查项目是否有本地修改
		if !opts.Force {
			// 创建一个临时的Git仓库对象来检查状态
			repo := git.NewRepository(projectPath, git.NewCommandRunner())
			
			// 检查是否有本地修改
			clean, err := repo.IsClean()
			if err != nil {
				return fmt.Errorf("failed to check if project %s is clean: %w", name, err)
			}
			
			if !clean {
				fmt.Printf("Project %s has local changes, skipping (use --force to override)\n", name)
				continue
			}
		}
		
		// 删除项目目录
		if err := os.RemoveAll(projectPath); err != nil {
			return fmt.Errorf("failed to remove project %s: %w", name, err)
		}
		
		fmt.Printf("Pruned project %s\n", name)
	}

	fmt.Println("Pruning completed successfully")
	_ = projects // Use projects variable
	return nil
}