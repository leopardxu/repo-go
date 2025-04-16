package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// CheckoutOptions 包含checkout命令的选项
type CheckoutOptions struct {
	CommonManifestOptions
	Quiet     bool
	Detach    bool
	Track     bool
	Force     bool
	Orphan    string
	Jobs      int
	Verbose   bool
	OuterManifest     bool
	NoOuterManifest   bool
	ThisManifestOnly  bool
	NoThisManifestOnly bool
}

// CheckoutCmd 返回checkout命令
func CheckoutCmd() *cobra.Command {
	opts := &CheckoutOptions{}

	cmd := &cobra.Command{
		Use:   "checkout <branchname> [<project>...]",
		Short: "Checkout an existing branch",
		Long:  `The 'repo checkout' command checks out an existing branch that was previously
created by 'repo start'.

The command is equivalent to:
repo forall [<project>...] -c git checkout <branchname>`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheckout(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.Detach, "detach", false, "detach HEAD at named commit")
	cmd.Flags().BoolVarP(&opts.Track, "track", "t", false, "set upstream info for new branch")
	cmd.Flags().BoolVarP(&opts.Force, "force", "f", false, "force checkout (throw away local modifications)")
	cmd.Flags().StringVar(&opts.Orphan, "orphan", "", "new unparented branch")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	opts.CommonManifestOptions = CommonManifestOptions{
		OuterManifest:    opts.OuterManifest,
		NoOuterManifest:  opts.NoOuterManifest,
		ThisManifestOnly: opts.ThisManifestOnly,
		AllManifests:     !opts.NoThisManifestOnly,
	}
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runCheckout 执行checkout命令
func runCheckout(opts *CheckoutOptions, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("branch name required")
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

	// 确定分支名和项目列表
	branchName := args[0]
	projectNames := args[1:]

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
		fmt.Printf("Checking out branch '%s' in projects\n", branchName)
	}

	// 构建checkout命令选项
	checkoutArgs := []string{"checkout"}
	
	if opts.Quiet {
		checkoutArgs = append(checkoutArgs, "--quiet")
	}
	
	if opts.Detach {
		checkoutArgs = append(checkoutArgs, "--detach")
	}
	
	if opts.Track {
		checkoutArgs = append(checkoutArgs, "--track")
	}
	
	if opts.Force {
		checkoutArgs = append(checkoutArgs, "--force")
	}
	
	if opts.Orphan != "" {
		checkoutArgs = append(checkoutArgs, "--orphan", opts.Orphan)
	}

	checkoutArgs = append(checkoutArgs, branchName)

	// 使用worker pool并发执行checkout
	errChan := make(chan error, len(projects))
	sem := make(chan struct{}, opts.Jobs)

	for _, p := range projects {
		sem <- struct{}{}
		go func(p *project.Project) {
			defer func() { <-sem }()
			
			if !opts.Quiet {
				fmt.Printf("\nProject %s:\n", p.Name)
			}
			
			// 执行checkout命令
			output, err := p.GitRepo.RunCommand(checkoutArgs...)
			if err != nil {
				errChan <- fmt.Errorf("project %s: %v", p.Name, err)
				return
			}
			
			if output != "" && !opts.Quiet {
				fmt.Println(output)
			}
			errChan <- nil
		}(p)
	}

	// 等待所有goroutine完成
	for i := 0; i < len(projects); i++ {
		if err := <-errChan; err != nil {
			if !opts.Quiet {
				fmt.Println(err)
			}
		}
	}

	return nil
}