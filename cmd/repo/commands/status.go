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
// runStatus executes the status command logic
func runStatus(opts *StatusOptions, args []string) error {
	// 加载配置
	cfg, err := config.Load() // Declare err here
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName) // Reuse err, no :=
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg) // Assuming manifest and cfg are loaded

	// 获取要处理的项目
	var projects []*project.Project // Declare projects
	// err is already declared above

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

	// TODO: Implement the logic to get and display status for the 'projects'
	fmt.Printf("Getting status for %d projects...\n", len(projects)) // Example usage
	// Replace with actual status checking and printing logic

	if !opts.Quiet {
		fmt.Println("Status command needs implementation.")
	}

	return nil // Placeholder
}