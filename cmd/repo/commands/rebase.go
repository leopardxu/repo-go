package commands

import (
	"fmt"
	"strings"

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
// runRebase executes the rebase command logic
func runRebase(opts *RebaseOptions, args []string) error {
	// 加载配置
	cfg, err := config.Load() // First declaration of err
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
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
	manager := project.NewManager(manifest, cfg) // Assuming manifest and cfg are loaded

	var projects []*project.Project
	// Don't redeclare err here
	
	if len(args) == 0 {
		projects, err = manager.GetProjects(nil) // Use = instead of :=
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(args) // Use = instead of :=
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
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
		
		// Define upstream variable before using it
		upstream := "origin" // Default value, adjust as needed
		// Or determine it dynamically based on project configuration
		// upstream := project.Remote
		
		fmt.Printf("Rebasing onto %s\n", upstream)
	}

	if !opts.Quiet {
		fmt.Println("Rebasing projects")
	}

	// 并发执行rebase操作
	type rebaseResult struct {
		Project *project.Project
		Output  string
		Err     error
	}

	results := make(chan rebaseResult, len(projects))
	sem := make(chan struct{}, 8) // 控制并发数为8

	for _, p := range projects {
		p := p
		go func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			
			outputBytes, err := p.GitRepo.RunCommand(rebaseArgs...)
			output := string(outputBytes)
			results <- rebaseResult{
				Project: p,
				Output:  output,
				Err:     err,
			}
		}()
	}

	var hasError bool
	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			hasError = true
			if opts.Verbose {
				fmt.Printf("Error in project %s: %v\n", res.Project.Name, res.Err)
			}
			if opts.FailFast {
				return fmt.Errorf("failed to rebase project %s: %w", res.Project.Name, res.Err)
			}
			continue
		}
		
		if !opts.Quiet {
			fmt.Printf("\nProject %s:\n", res.Project.Name)
			if res.Output != "" {
				fmt.Println(res.Output)
			} else {
				fmt.Println("Rebase completed successfully")
			}
		}
	}

	if hasError {
		return fmt.Errorf("some projects failed to rebase")
	}
	return nil
}