package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// CherryPickOptions holds the options for the cherry-pick command
type CherryPickOptions struct {
	All            bool
	Jobs           int
	Quiet          bool
	Verbose        bool
	Config         *config.Config
	CommonManifestOptions
}

// CherryPickCmd creates the cherry-pick command
func CherryPickCmd() *cobra.Command {
	opts := &CherryPickOptions{}
	cmd := &cobra.Command{
		Use:   "cherry-pick <commit> [<project>...]",
		Short: "Cherry-pick a commit onto the current branch",
		Long:  `Applies the changes introduced by the named commit(s) onto the current branch.`,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg
			return runCherryPick(opts, args)
		},
	}
	cmd.Flags().BoolVar(&opts.All, "all", false, "cherry-pick in all projects")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of projects to cherry-pick in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}

// runCherryPick executes the cherry-pick command logic
func runCherryPick(opts *CherryPickOptions, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("missing commit hash")
	}
	commit := args[0]
	projectNames := args[1:]
	cfg := opts.Config
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName)
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

type cherryPickResult struct {
	ProjectName string
	Err        error
}
	results := make(chan cherryPickResult, len(projects))
	sem := make(chan struct{}, opts.Jobs)
	for _, p := range projects {
		p := p
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			_, err := p.GitRepo.RunCommand("cherry-pick", commit)
			results <- cherryPickResult{ProjectName: p.Name, Err: err}
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
		fmt.Printf("Cherry-pick commit '%s': %d success, %d failed\n", commit, success, failed)
	}
	if failed > 0 {
		return fmt.Errorf("cherry-pick failed for %d projects", failed)
	}
	return nil
}