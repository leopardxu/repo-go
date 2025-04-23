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
			return runBranch(opts, args)
		},
	}

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
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	manager := project.NewManager(manifestObj, cfg)

	var projects []*project.Project
	if len(args) == 0 {
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	type branchResult struct {
		ProjectName string
		CurrentBranch string
		Branches []string
		Err error
	}
	results := make(chan branchResult, len(projects))
	sem := make(chan struct{}, opts.Jobs)
	for _, p := range projects {
		p := p
		sem <- struct{}{}
		go func() {
			defer func() { <-sem }()
			currentBranch, err := p.GitRepo.RunCommand("rev-parse", "--abbrev-ref", "HEAD")
			if err != nil {
				results <- branchResult{ProjectName: p.Name, Err: err}
				return
			}
			branchesOutput, err := p.GitRepo.RunCommand("branch", "--list")
			if err != nil {
				results <- branchResult{ProjectName: p.Name, Err: err}
				return
			}
			branches := strings.Split(strings.TrimSpace(branchesOutput), "\n")
			results <- branchResult{ProjectName: p.Name, CurrentBranch: strings.TrimSpace(currentBranch), Branches: branches}
		}()
	}
	branchInfo := make(map[string][]string)
	currentBranches := make(map[string]bool)
	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			if !opts.Quiet {
				fmt.Printf("Error getting branches for %s: %v\n", res.ProjectName, res.Err)
			}
			continue
		}
		currentBranches[res.CurrentBranch] = true
		for _, branch := range res.Branches {
			branch = strings.TrimSpace(branch)
			if branch == "" {
				continue
			}
			branchInfo[branch] = append(branchInfo[branch], res.ProjectName)
		}
	}
	for branch, projs := range branchInfo {
		if currentBranches[branch] {
			fmt.Print("*")
		} else {
			fmt.Print(" ")
		}
		fmt.Print(" ")
		fmt.Printf(" %-30s", branch)
		if len(projs) < len(projects) {
			fmt.Printf(" | in %s", strings.Join(projs, ", "))
		}
		fmt.Println()
	}
	return nil
}