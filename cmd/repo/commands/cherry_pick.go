package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// CherryPickOptions 包含cherry-pick命令的选项
type CherryPickOptions struct {
	Edit      bool
	NoCommit  bool
	Signoff   bool
	Strategy  string
	Mainline  int
	AllowEmpty bool
	KeepRedundantCommits bool
	Verbose   bool
	Quiet     bool
	OuterManifest bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// CherryPickCmd 返回cherry-pick命令
func CherryPickCmd() *cobra.Command {
	opts := &CherryPickOptions{}

	cmd := &cobra.Command{
		Use:   "cherry-pick <sha1>",
		Short: "Cherry-pick a change",
		Long:  `Apply the changes introduced by some existing commits to the current branch.\nThe change id will be updated, and a reference to the old change id will be added.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCherryPick(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Edit, "edit", "e", false, "edit commit message before committing")
	cmd.Flags().BoolVarP(&opts.NoCommit, "no-commit", "n", false, "don't automatically commit")
	cmd.Flags().BoolVarP(&opts.Signoff, "signoff", "s", false, "add Signed-off-by line at the end of the commit message")
	cmd.Flags().StringVar(&opts.Strategy, "strategy", "", "merge strategy to use")
	cmd.Flags().IntVar(&opts.Mainline, "mainline", 0, "select mainline parent")
	cmd.Flags().BoolVar(&opts.AllowEmpty, "allow-empty", false, "allow empty commits")
	cmd.Flags().BoolVar(&opts.KeepRedundantCommits, "keep-redundant-commits", false, "keep redundant, empty commits")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runCherryPick 执行cherry-pick命令
func runCherryPick(opts *CherryPickOptions, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("commit required")
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

	// 确定提交和项目列表
	var commits []string
	var projectNames []string

	// 假设最后一个或多个参数是提交ID
	// 这里简化处理，实际上可能需要更复杂的逻辑来区分项目名和提交ID
	commits = []string{args[len(args)-1]}
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

	fmt.Printf("Cherry-picking commit '%s' in projects\n", commits[0])

	// 构建cherry-pick命令选项
	cherryPickArgs := []string{"cherry-pick"}
	
	if opts.Edit {
		cherryPickArgs = append(cherryPickArgs, "--edit")
	}
	
	if opts.NoCommit {
		cherryPickArgs = append(cherryPickArgs, "--no-commit")
	}
	
	if opts.Signoff {
		cherryPickArgs = append(cherryPickArgs, "--signoff")
	}
	
	if opts.Strategy != "" {
		cherryPickArgs = append(cherryPickArgs, "--strategy", opts.Strategy)
	}
	
	if opts.Mainline != 0 {
		cherryPickArgs = append(cherryPickArgs, "--mainline", fmt.Sprintf("%d", opts.Mainline))
	}
	
	if opts.AllowEmpty {
		cherryPickArgs = append(cherryPickArgs, "--allow-empty")
	}
	
	if opts.KeepRedundantCommits {
		cherryPickArgs = append(cherryPickArgs, "--keep-redundant-commits")
	}

	cherryPickArgs = append(cherryPickArgs, commits[0])

	// 对每个项目执行cherry-pick
	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("\nProject %s:\n", p.Name)
		}
		
		// 执行cherry-pick命令
		output, err := p.GitRepo.RunCommand(cherryPickArgs...)
		if err != nil {
			if !opts.Quiet {
				fmt.Printf("Error in project %s: %v\n", p.Name, err)
			}
			continue
		}
		
		if output != "" && !opts.Quiet {
			fmt.Println(output)
		} else if !opts.Quiet {
			fmt.Println("Cherry-pick completed successfully")
		}
	}

	return nil
}