package commands

import (
	"fmt"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// BranchOptions holds the options for the branch command
type BranchOptions struct {
	All       bool
	Current   bool
	Color     string
	List      bool
	Verbose   bool
	SetUpstream string
	Jobs      int
	Quiet     bool
	Config    *config.Config // <-- Add this field
	CommonManifestOptions
}

// BranchCmd creates the branch command
func BranchCmd() *cobra.Command {
	opts := &BranchOptions{}

	cmd := &cobra.Command{
		Use:   "branches [<project>...]",
		Short: "View current topic branches",
		Long:  `Summarizes the currently available topic branches.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config if not already in opts
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			return runBranch(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.All, "all", "a", false, "show all branches") 
	cmd.Flags().BoolVar(&opts.Current, "current", false, "consider only the current branch") 
	cmd.Flags().StringVar(&opts.Color, "color", "auto", "control color usage: auto, always, never")
	cmd.Flags().BoolVarP(&opts.List, "list", "l", false, "list branches")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show hash and subject, give twice for upstream branch")
	cmd.Flags().StringVar(&opts.SetUpstream, "set-upstream", "", "set upstream for git pull/fetch")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &CommonManifestOptions{})

	return cmd
}

// runBranch executes the branch command logic
func runBranch(opts *BranchOptions, args []string) error {
	// 加载配置
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName) // Use opts.Config
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	manager := project.NewManager(manifest, opts.Config) // Use opts.Config
	// Declare projects variable once
	var projects []*project.Project // <-- Declare projects variable here

	// 获取要处理的项目
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects(nil) // <-- Use nil instead of "" and assign with =
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(args) // <-- Assign with =
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 收集所有分支信息
	branchInfo := make(map[string][]string)
	currentBranches := make(map[string]bool)

	for _, p := range projects {
		// 获取当前分支
		currentBranch, err := p.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			if !opts.Quiet {
				fmt.Printf("Error getting current branch for %s: %v\n", p.Name, err)
			}
			continue
		}
		currentBranches[strings.TrimSpace(currentBranch)] = true

		// 获取所有分支
		branchesOutput, err := p.GitRepo.RunCommand("branch", "--list")
		branches := strings.Split(strings.TrimSpace(branchesOutput), "\n")
		if err != nil {
			if !opts.Quiet {
				fmt.Printf("Error getting branches for %s: %v\n", p.Name, err)
			}
			continue
		}

		for _, branch := range branches {
			branchInfo[branch] = append(branchInfo[branch], p.Name)
		}
	}

	// 显示分支信息
	for branch, projects := range branchInfo {
		// 第一列：当前分支标记
		if currentBranches[branch] {
			fmt.Print("*")
		} else {
			fmt.Print(" ")
		}

		// 第二列：上传状态（暂留空）
		fmt.Print(" ") 

		// 第三列：分支名
		fmt.Printf(" %-30s", branch)

		// 第四列：项目列表
		if len(projects) < len(projects) {
			fmt.Printf(" | in %s", strings.Join(projects, ", "))
		}
		fmt.Println()
	}

	return nil
}