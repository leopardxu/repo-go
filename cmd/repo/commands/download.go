package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DownloadOptions 包含download命令的选项
type DownloadOptions struct {
	CherryPick bool
	FFOnly     bool
	NoTags     bool
	Rebase     bool
	Jobs       int
	Branch     string
	RecordOrigin bool
	Revert     bool
	Verbose    bool
	Quiet      bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// DownloadCmd 返回download命令
func DownloadCmd() *cobra.Command {
	opts := &DownloadOptions{}

	cmd := &cobra.Command{
		Use:   "download {[project] change[/patchset]}...",
		Short: "Download and checkout a change",
		Long:  `The 'repo download' command downloads a change from the review system and makes
it available in your project's local working directory. If no project is
specified try to use current directory as a project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDownload(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.CherryPick, "cherry-pick", "c", false, "cherry-pick instead of checkout")
	cmd.Flags().BoolVar(&opts.FFOnly, "ff-only", false, "force fast-forward merge")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", false, "don't fetch tags")
	cmd.Flags().BoolVarP(&opts.Rebase, "rebase", "r", false, "rebase instead of checkout")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 1, "number of jobs to run simultaneously")
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "create a new branch first")
	cmd.Flags().BoolVar(&opts.RecordOrigin, "record-origin", false, "pass -x when cherry-picking")
	cmd.Flags().BoolVar(&opts.Revert, "revert", false, "revert instead of checkout")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runDownload 执行download命令
func runDownload(opts *DownloadOptions, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("change ID required")
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

	// 确定变更ID和项目列表
	var changeIDs []string
	var projectNames []string

	// 假设最后一个或多个参数是变更ID
	// 这里简化处理，实际上可能需要更复杂的逻辑来区分项目名和变更ID
	changeIDs = []string{args[len(args)-1]}
	if len(args) > 1 {
		projectNames = args[:len(args)-1]
	}

	// 获取要处理的项目
	var projects []*project.Project
	if len(projectNames) == 0 {
		// 如果没有指定项目，则处理所有项目
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

	if !opts.Quiet {
		fmt.Printf("Downloading change '%s' from Gerrit\n", changeIDs[0])
	}

	// 这里应该实现从Gerrit下载变更的逻辑
	// 由于这需要与特定的Gerrit实例交互，这里只提供一个框架

	// 对每个项目执行下载
	for _, p := range projects {
		fmt.Printf("\nProject %s:\n", p.Name)
		
		// 这里应该实现实际的下载逻辑
		// 例如，使用git fetch命令从Gerrit获取变更
		// 然后根据选项执行checkout、cherry-pick或rebase
		
		// 示例：
		// 1. 获取变更
		fetchArgs := []string{"fetch", "origin", fmt.Sprintf("refs/changes/%s", changeIDs[0])}
		if opts.NoTags {
			fetchArgs = append(fetchArgs, "--no-tags")
		}
		
		output, err := p.GitRepo.RunCommand(fetchArgs...)
		if err != nil {
			fmt.Printf("Error fetching change in project %s: %v\n", p.Name, err)
			continue
		}
		
		// 2. 根据选项应用变更
		var applyArgs []string
		if opts.CherryPick {
			applyArgs = []string{"cherry-pick"}
			if opts.RecordOrigin {
				applyArgs = append(applyArgs, "-x")
			}
			applyArgs = append(applyArgs, "FETCH_HEAD")
		} else if opts.Rebase {
			applyArgs = []string{"rebase", "FETCH_HEAD"}
		} else if opts.Revert {
			applyArgs = []string{"revert", "FETCH_HEAD"}
		} else {
			applyArgs = []string{"checkout", "FETCH_HEAD"}
			if opts.FFOnly {
				applyArgs = append(applyArgs, "--ff-only")
			}
		}
		
		output, err = p.GitRepo.RunCommand(applyArgs...)
		if err != nil {
			fmt.Printf("Error applying change in project %s: %v\n", p.Name, err)
			continue
		}
		
		if output != "" {
			fmt.Println(output)
		} else {
			fmt.Println("Change downloaded and applied successfully")
		}
	}

	return nil
}