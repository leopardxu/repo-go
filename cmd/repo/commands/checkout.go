package commands

import (
	"fmt"
	"strings"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// CheckoutOptions holds the options for the checkout command
// 优化参数结构体，增加与 start/branch 命令一致的参数
// 支持 --all, --jobs, --quiet, --verbose
// 支持 --branch 指定分支名
// 支持 --detach, --force-sync, --force-overwrite
// 支持 Manifest 相关参数
type CheckoutOptions struct {
	Detach         bool
	ForceSync      bool
	ForceOverwrite bool
	JobsCheckout   int
	Quiet          bool
	Verbose        bool
	All            bool
	Branch         string
	Config         *config.Config
	CommonManifestOptions
}

// CheckoutCmd creates the checkout command
func CheckoutCmd() *cobra.Command {
	opts := &CheckoutOptions{}
	cmd := &cobra.Command{
		Use:   "checkout <branch> [<project>...]",
		Short: "Checkout a branch for development",
		Long:  `Checks out a branch for development, creating it if necessary.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			return runCheckout(opts, args)
		},
	}
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force overwrite existing files")
	cmd.Flags().IntVarP(&opts.JobsCheckout, "jobs", "j", 8, "number of projects to checkout in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVar(&opts.All, "all", false, "checkout branch in all projects")
	cmd.Flags().StringVarP(&opts.Branch, "branch", "b", "", "specify an alternate branch name")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}

// runCheckout executes the checkout command logic
func runCheckout(opts *CheckoutOptions, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing branch name")
	}
	branchName := args[0]
	if opts.Branch != "" {
		branchName = opts.Branch
	}
	projectNames := args[1:]
	cfg := opts.Config
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	manager := project.NewManager(manifestObj, cfg)
	var projects []*project.Project
	if opts.All || len(projectNames) == 0 {
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(projectNames)
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}
	type checkoutResult struct {
		ProjectName string
		Err        error
	}
	results := make(chan checkoutResult, len(projects))
	sem := make(chan struct{}, opts.JobsCheckout)
	for _, p := range projects {
		p := p
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			// checkout 分支
			if opts.Detach {
				_, err := p.GitRepo.RunCommand("checkout", p.Revision)
				results <- checkoutResult{ProjectName: p.Name, Err: err}
				return
			}
			// force sync/overwrite 可扩展
			_, err := p.GitRepo.RunCommand("checkout", "-B", branchName)
			results <- checkoutResult{ProjectName: p.Name, Err: err}
		}()
	}
	success, failed := 0, 0
	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			failed++
			if !opts.Quiet {
				fmt.Printf("[FAILED] %s: %v\n", res.ProjectName, res.Err)
			}
		} else {
			success++
			if opts.Verbose && !opts.Quiet {
				fmt.Printf("[OK] %s\n", res.ProjectName)
			}
		}
	}
	if !opts.Quiet {
		fmt.Printf("Checkout branch '%s': %d success, %d failed\n", branchName, success, failed)
	}
	if failed > 0 {
		return fmt.Errorf("checkout failed for %d projects", failed)
	}
	return nil
}