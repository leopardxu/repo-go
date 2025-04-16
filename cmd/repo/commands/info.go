package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// InfoOptions 包含info命令的选项
type InfoOptions struct {
	Diff            bool
	Overview        bool
	CurrentBranch   bool
	NoCurrentBranch bool
	LocalOnly       bool
	Verbose         bool
	Quiet           bool
	OuterManifest   bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// InfoCmd 返回info命令
func InfoCmd() *cobra.Command {
	opts := &InfoOptions{}

	cmd := &cobra.Command{
		Use:   "info [-dl] [-o [-c]] [<project>...]",
		Short: "Get info on the manifest branch, current branch or unmerged branches",
		Long:  `Show detailed information about projects including branch info.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runInfo(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Diff, "diff", "d", false, "show full info and commit diff including remote branches")
	cmd.Flags().BoolVarP(&opts.Overview, "overview", "o", false, "show overview of all local commits")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "consider only checked out branches")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "consider all local branches")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "disable all remote operations")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runInfo 执行info命令
func runInfo(opts *InfoOptions, args []string) error {
	fmt.Println("Showing info for projects")

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

	// 对每个项目显示信息
	for _, p := range projects {
		fmt.Printf("\nProject %s:\n", p.Name)
		
		// 显示项目基本信息
		fmt.Printf("Path: %s\n", p.Path)
		// 使用 p.RemoteName 而不是 p.Remote
		fmt.Printf("Remote: %s\n", p.RemoteName)
		
		// 获取当前分支
		currentBranch, err := p.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			fmt.Printf("Error getting current branch: %v\n", err)
		} else {
			fmt.Printf("Current branch: %s\n", currentBranch)
		}
		
		// 根据选项显示不同的信息
		if opts.Diff {
			// 显示完整信息和提交差异
			showDiff(p)
		} else if opts.LocalOnly {
			// 显示本地分支信息
			showLocalBranches(p)
		} else if opts.OuterManifest {
			// 显示远程分支信息
			showRemoteBranches(p)
		} else if opts.CurrentBranch {
			// 显示当前分支信息
			showCurrentBranchInfo(p)
		} else if opts.Diff {
			// 显示所有分支信息
			showAllBranches(p)
		} else if opts.Overview {
			// 显示所有本地提交的概览
			showCommitOverview(p)
		} else {
			// 默认显示基本信息
			showBasicInfo(p)
		}
	}

	return nil
}

// showDiff 显示完整信息和提交差异
func showDiff(p *project.Project) {
	fmt.Println("Commit differences:")
	
	// 获取本地和远程分支之间的差异
	output, err := p.GitRepo.RunCommand("log", "--oneline", "HEAD..@{upstream}")
	if err != nil {
		fmt.Printf("Error getting commit diff: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No differences found")
	}
}

// showLocalBranches 显示本地分支信息
func showLocalBranches(p *project.Project) {
	fmt.Println("Local branches:")
	
	output, err := p.GitRepo.RunCommand("branch")
	if err != nil {
		fmt.Printf("Error getting local branches: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No local branches found")
	}
}

// showRemoteBranches 显示远程分支信息
func showRemoteBranches(p *project.Project) {
	fmt.Println("Remote branches:")
	
	output, err := p.GitRepo.RunCommand("branch", "-r")
	if err != nil {
		fmt.Printf("Error getting remote branches: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No remote branches found")
	}
}

// showCurrentBranchInfo 显示当前分支信息
func showCurrentBranchInfo(p *project.Project) {
	fmt.Println("Current branch info:")
	
	// 获取当前分支的最近提交
	output, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting current branch info: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commit info found")
	}
}

// showAllBranches 显示所有分支信息
func showAllBranches(p *project.Project) {
	fmt.Println("All branches:")
	
	output, err := p.GitRepo.RunCommand("branch", "-a")
	if err != nil {
		fmt.Printf("Error getting all branches: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No branches found")
	}
}

// showCommitOverview 显示所有本地提交的概览
func showCommitOverview(p *project.Project) {
	fmt.Println("Commit overview:")
	
	output, err := p.GitRepo.RunCommand("log", "--oneline", "-10")
	if err != nil {
		fmt.Printf("Error getting commit overview: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commits found")
	}
}

// showBasicInfo 显示基本信息
func showBasicInfo(p *project.Project) {
	// 获取最近的提交
	output, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting latest commit: %v\n", err)
		return
	}
	
	fmt.Printf("Latest commit: %s\n", output)
	
	// 获取未提交的更改
	output, err = p.GitRepo.RunCommand("status", "--short")
	if err != nil {
		fmt.Printf("Error getting status: %v\n", err)
		return
	}
	
	if output != "" {
		fmt.Println("Uncommitted changes:")
		fmt.Println(output)
	} else {
		fmt.Println("Working directory clean")
	}
}