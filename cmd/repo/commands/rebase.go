package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// RebaseOptions 包含rebase命令的选项
type RebaseOptions struct {
	Abort          bool
	Continue       bool
	Skip           bool
	Interactive    bool
	Autosquash     bool
	Onto           string
	Force          bool
	FailFast       bool
	AutoStash      bool
	NoFF           bool
	Whitespace     string
	OntoManifest   bool
	Verbose        bool
	Quiet          bool
	OuterManifest  bool
	NoOuterManifest bool
	ThisManifestOnly bool
}

// RebaseCmd 返回rebase命令
func RebaseCmd() *cobra.Command {
	opts := &RebaseOptions{}

	cmd := &cobra.Command{
		Use:   "rebase {[<project>...] | -i <project>...}",
		Short: "Rebase local branches on upstream branch",
		Long:  `'repo rebase' uses git rebase to move local changes in the current topic branch
to the HEAD of the upstream history, useful when you have made commits in a
topic branch but need to incorporate new upstream changes "underneath" them.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runRebase(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVar(&opts.Abort, "abort", false, "abort current rebase")
	cmd.Flags().BoolVar(&opts.Continue, "continue", false, "continue current rebase")
	cmd.Flags().BoolVar(&opts.Skip, "skip", false, "skip current patch and continue")
	cmd.Flags().BoolVarP(&opts.Interactive, "interactive", "i", false, "interactive rebase")
	cmd.Flags().BoolVar(&opts.Autosquash, "autosquash", false, "automatically squash fixup commits")
	cmd.Flags().StringVar(&opts.Onto, "onto", "", "rebase onto given branch instead of upstream")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "force rebase even if branch is up to date")
	cmd.Flags().BoolVar(&opts.FailFast, "fail-fast", false, "stop rebasing after first error is hit")
	cmd.Flags().BoolVar(&opts.AutoStash, "auto-stash", false, "stash local modifications before starting")
	cmd.Flags().BoolVar(&opts.NoFF, "no-ff", false, "pass --no-ff to git rebase")
	cmd.Flags().StringVar(&opts.Whitespace, "whitespace", "", "pass --whitespace to git rebase")
	cmd.Flags().BoolVarP(&opts.OntoManifest, "onto-manifest", "m", false, "rebase onto the manifest version instead of upstream HEAD")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")

	return cmd
}

// runRebase 执行rebase命令
func runRebase(opts *RebaseOptions, args []string) error {
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

	// 处理多清单选项
	if opts.OuterManifest {
		manifest = manifest.GetOuterManifest()
	} else if opts.NoOuterManifest {
		manifest = manifest.GetInnerManifest()
	}

	if opts.ThisManifestOnly {
		manifest = manifest.GetThisManifest()
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg)

	// 确定上游分支和项目列表
	var upstream string
	var projectNames []string

	if len(args) > 0 {
		// 最后一个参数可能是上游分支
		upstream = args[len(args)-1]
		if len(args) > 1 {
			projectNames = args[:len(args)-1]
		}
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

	// 构建rebase命令选项
	rebaseArgs := []string{"rebase"}
	
	if opts.Abort {
		rebaseArgs = append(rebaseArgs, "--abort")
	} else if opts.Continue {
		rebaseArgs = append(rebaseArgs, "--continue")
	} else if opts.Skip {
		rebaseArgs = append(rebaseArgs, "--skip")
	} else {
		if opts.Interactive {
			rebaseArgs = append(rebaseArgs, "--interactive")
		}
		
		if opts.Autosquash {
			rebaseArgs = append(rebaseArgs, "--autosquash")
		}
		
		if opts.Onto != "" {
			rebaseArgs = append(rebaseArgs, "--onto", opts.Onto)
		}
		
		if opts.Force {
			rebaseArgs = append(rebaseArgs, "--force")
		}
		
		if opts.NoFF {
			rebaseArgs = append(rebaseArgs, "--no-ff")
		}
		
		if opts.Whitespace != "" {
			rebaseArgs = append(rebaseArgs, "--whitespace", opts.Whitespace)
		}
		
		if opts.AutoStash {
			rebaseArgs = append(rebaseArgs, "--autostash")
		}
		
		if upstream != "" {
			rebaseArgs = append(rebaseArgs, upstream)
		}
	}

	if !opts.Quiet {
		fmt.Println("Rebasing projects")
	}

	// 对每个项目执行rebase
	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("\nProject %s:\n", p.Name)
		}
		
		// 执行rebase命令
		output, err := p.GitRepo.RunCommand(rebaseArgs...)
		if err != nil {
			if opts.Verbose {
				fmt.Printf("Error in project %s: %v\n", p.Name, err)
			}
			if opts.FailFast {
				return fmt.Errorf("failed to rebase project %s: %w", p.Name, err)
			}
			continue
		}
		
		if output != "" && !opts.Quiet {
			fmt.Println(output)
		} else if !opts.Quiet {
			fmt.Println("Rebase completed successfully")
		}
	}

	return nil
}