package commands

import (
	"fmt"
	"runtime"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// StatusOptions 包含status命令的选项
type StatusOptions struct {
	CommonManifestOptions
	Jobs              int
	Orphans           bool
	Quiet             bool
	Verbose           bool
	Branch            bool
}

// StatusCmd 返回status命令
func StatusCmd() *cobra.Command {
	opts := &StatusOptions{
		Jobs: runtime.NumCPU() * 2,
	}

	cmd := &cobra.Command{
		Use:   "status [<project>...]",
		Short: "Show the working tree status",
		Long:  `Show the status of the working tree. This includes projects with uncommitted changes, projects with unpushed commits, and projects on different branches than specified in the manifest.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of jobs to run in parallel (default: 8; based on number of CPU cores)")
	cmd.Flags().BoolVarP(&opts.Orphans, "orphans", "o", false, "include objects in working directory outside of repo projects")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runStatus 执行status命令
func runStatus(opts *StatusOptions, args []string) error {
	if !opts.Quiet {
		fmt.Println("Checking status of projects")
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

	// 检查每个项目的状态
	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("Checking status of project '%s'\n", p.Name)
		}
		
		// 获取项目状态
		status, err := p.GitRepo.Status()
		if err != nil {
			return fmt.Errorf("failed to get status of project %s: %w", p.Name, err)
		}
		
		// 如果项目有修改，显示状态
		// 将 status != "" 改为 len(status) > 0 或 string(status) != ""
		if len(status) > 0 {
			fmt.Printf("project %s:\n%s\n", p.Name, status)
		}
		
		// 如果需要显示分支信息
		if opts.Branch {
			branch, err := p.GitRepo.CurrentBranch()
			if err != nil {
				return fmt.Errorf("failed to get current branch of project %s: %w", p.Name, err)
			}
			
			if branch != p.Revision {
				fmt.Printf("project %s: branch %s (manifest: %s)\n", p.Name, branch, p.Revision)
			}
		}
	}

	if !opts.Quiet {
		fmt.Println("Status check completed")
	}
	return nil
}