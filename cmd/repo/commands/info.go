package commands

import (
	"fmt"
	"strings"

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
    Config *config.Config // <-- Add this field
    CommonManifestOptions
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
// runInfo executes the info command logic
func runInfo(opts *InfoOptions, args []string) error {
    // Load config
    cfg, err := config.Load() // Declare err here
    if err != nil {
        return fmt.Errorf("failed to load config: %w", err)
    }
    opts.Config = cfg // Assign loaded config

    // Load manifest
    parser := manifest.NewParser()
    manifest, err := parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,",")) // Reuse err
    if err != nil {
        return fmt.Errorf("failed to parse manifest: %w", err)
    }

    // Create project manager
    manager := project.NewManager(manifest, cfg)

    // Declare projects variable once
    var projects []*project.Project // Declare projects

    // Get projects to operate on
    if len(args) == 0 {
        projects, err = manager.GetProjects(nil) // Use =, use nil
        if err != nil {
            return fmt.Errorf("failed to get projects: %w", err)
        }
    } else {
        projects, err = manager.GetProjectsByNames(args) // Use =
        if err != nil {
            return fmt.Errorf("failed to get projects by name: %w", err)
        }
    }

    // 并发获取项目信息
type infoResult struct {
	Project *project.Project
	Output  string
	Err     error
}

results := make(chan infoResult, len(projects))
sem := make(chan struct{}, 8) // 控制并发数

for _, p := range projects {
	sem <- struct{}{}
	go func(proj *project.Project) {
		defer func() { <-sem }()
		
		var output string
		var err error
		var outputBytes []byte
		
		// 根据选项显示不同信息
		switch {
		case opts.Diff:
			outputBytes, err = proj.GitRepo.RunCommand("log", "--oneline", "HEAD..@{upstream}")
			if err == nil {
				output = strings.TrimSpace(string(outputBytes))
			}
		case opts.Overview:
			outputBytes, err = proj.GitRepo.RunCommand("log", "--oneline", "-10")
			if err == nil {
				output = strings.TrimSpace(string(outputBytes))
			}
		case opts.CurrentBranch:
			outputBytes, err = proj.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
			if err == nil {
				output = strings.TrimSpace(string(outputBytes))
			}
		default:
			outputBytes, err = proj.GitRepo.RunCommand("status", "--short")
			if err == nil {
				output = strings.TrimSpace(string(outputBytes))
			}
		}
		
		results <- infoResult{Project: proj, Output: output, Err: err}
	}(p)
}

// 收集并显示结果
for i := 0; i < len(projects); i++ {
	res := <-results
	if res.Err != nil {
		if !opts.Quiet {
			fmt.Printf("Error getting info for %s: %v\n", res.Project.Name, res.Err)
		}
		continue
	}
	
	if res.Output != "" {
		fmt.Printf("--- %s ---\n%s\n", res.Project.Name, res.Output)
	} else if !opts.Quiet {
		fmt.Printf("--- %s ---\n(No changes)\n", res.Project.Name)
	}
}

return nil
}

// showDiff 显示完整信息和提交差异
func showDiff(p *project.Project) {
	fmt.Println("Commit differences:")
	
	// 获取本地和远程分支之间的差异
	outputBytes, err := p.GitRepo.RunCommand("log", "--oneline", "HEAD..@{upstream}")
	if err != nil {
		fmt.Printf("Error getting commit diff: %v\n", err)
		return
	}
	
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No differences found")
	}
}

// showLocalBranches 显示本地分支信息
func showLocalBranches(p *project.Project) {
	fmt.Println("Local branches:")
	
	outputBytes, err := p.GitRepo.RunCommand("branch")
	if err != nil {
		fmt.Printf("Error getting local branches: %v\n", err)
		return
	}
	
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No local branches found")
	}
}

// showRemoteBranches 显示远程分支信息
func showRemoteBranches(p *project.Project) {
	fmt.Println("Remote branches:")
	
	outputBytes, err := p.GitRepo.RunCommand("branch", "-r")
	if err != nil {
		fmt.Printf("Error getting remote branches: %v\n", err)
		return
	}
	output := strings.TrimSpace(string(outputBytes))
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
	outputBytes, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting current branch info: %v\n", err)
		return
	}
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commit info found")
	}
}

// showAllBranches 显示所有分支信息
func showAllBranches(p *project.Project) {
	fmt.Println("All branches:")
	
	outputBytes, err := p.GitRepo.RunCommand("branch", "-a")
	if err != nil {
		fmt.Printf("Error getting all branches: %v\n", err)
		return
	}
	
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No branches found")
	}
}

// showCommitOverview 显示所有本地提交的概览
func showCommitOverview(p *project.Project) {
	fmt.Println("Commit overview:")
	
	outputBytes, err := p.GitRepo.RunCommand("log", "--oneline", "-10")
	if err != nil {
		fmt.Printf("Error getting commit overview: %v\n", err)
		return
	}
	
	output := strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println(output)
	} else {
		fmt.Println("No commits found")
	}
}

// showBasicInfo 显示基本信息
func showBasicInfo(p *project.Project) {
	// 获取最近的提交
	outputBytes, err := p.GitRepo.RunCommand("log", "-1", "--oneline")
	if err != nil {
		fmt.Printf("Error getting latest commit: %v\n", err)
		return
	}
	
	output := strings.TrimSpace(string(outputBytes))
	fmt.Printf("Latest commit: %s\n", output)
	
	// 获取未提交的更改
	outputBytes, err = p.GitRepo.RunCommand("status", "--short")
	if err != nil {
		fmt.Printf("Error getting status: %v\n", err)
		return
	}
	
	output = strings.TrimSpace(string(outputBytes))
	if output != "" {
		fmt.Println("Uncommitted changes:")
		fmt.Println(output)
	} else {
		fmt.Println("Working directory clean")
	}
}