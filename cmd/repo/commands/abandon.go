package commands

import (
	"fmt"
	"runtime"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
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
	// 获取分支名称
	branchName := args[0]
	
	// 获取项目列表
	projectNames := args[1:]
	
	// 如果指定了--project选项，则添加到项目列表中
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
	if len(projectNames) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		var projectsErr error
		projects, projectsErr = manager.GetProjectsByNames(projectNames)
		if projectsErr != nil {
			return fmt.Errorf("failed to get projects by names: %w", projectsErr)
		}
	}
	
	// 在每个项目中放弃分支
	for _, p := range projects {
		// 检查分支是否存在
		exists, err := p.GitRepo.BranchExists(branchName)
		if err != nil {
			return fmt.Errorf("failed to check if branch exists in project %s: %w", p.Name, err)
		}
		
		if !exists {
			if !opts.Quiet {
				fmt.Printf("Branch '%s' does not exist in project '%s', skipping\n", branchName, p.Name)
			}
			continue
		}
		
		if !opts.Quiet {
			if opts.DryRun {
				fmt.Printf("Would abandon branch '%s' in project '%s'\n", branchName, p.Name)
			} else {
				fmt.Printf("Abandoning branch '%s' in project '%s'\n", branchName, p.Name)
			}
		}
		
		// 如果不是模拟运行，则实际放弃分支
		if !opts.DryRun {
			// 检查当前分支
			currentBranch, err := p.GitRepo.CurrentBranch()
			if err != nil {
				return fmt.Errorf("failed to get current branch of project %s: %w", p.Name, err)
			}
			
			// 如果当前分支是要放弃的分支，则先切换到清单中指定的分支
			if currentBranch == branchName {
				if !opts.Quiet {
					fmt.Printf("Switching to branch '%s' in project '%s'\n", p.Revision, p.Name)
				}
				
				if err := p.GitRepo.Checkout(p.Revision); err != nil {
					return fmt.Errorf("failed to checkout branch in project %s: %w", p.Name, err)
				}
			}
			
			// 删除分支
			if err := p.GitRepo.DeleteBranch(branchName, opts.Force); err != nil {
				return fmt.Errorf("failed to delete branch in project %s: %w", p.Name, err)
			}
		}
	}

	if !opts.Quiet {
		if opts.DryRun {
			fmt.Printf("Would have abandoned branch '%s' successfully\n", branchName)
		} else {
			fmt.Printf("Branch '%s' abandoned successfully\n", branchName)
		}
	}
	return nil
}