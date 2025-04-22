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
	// Add specific options for cherry-pick if needed
	Quiet  bool
	Config *config.Config // <-- Add Config field
	CommonManifestOptions
}

// CherryPickCmd creates the cherry-pick command
func CherryPickCmd() *cobra.Command {
	opts := &CherryPickOptions{}
	cmd := &cobra.Command{
		Use:   "cherry-pick <commit> [<project>...]",
		Short: "Cherry-pick a commit onto the current branch",
		Long:  `Applies the changes introduced by the named commit(s) onto the current branch.`,
		Args:  cobra.MinimumNArgs(1), // Requires at least the commit hash
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			// Pass commit (args[0]) and optional project names (args[1:])
			return runCherryPick(opts, args[0], args[1:])
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runCherryPick executes the cherry-pick command logic
func runCherryPick(opts *CherryPickOptions, commit string, projectNames []string) error {
	// Load manifest
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Create project manager
	manager := project.NewManager(manifest, opts.Config)

	// Declare projects variable once
	var projects []*project.Project

	// Get projects to operate on
	if len(projectNames) == 0 {
		projects, err = manager.GetProjects(nil) // <-- Use nil, assign with =
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(projectNames) // <-- Assign with =
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	// Perform cherry-pick operation
	if !opts.Quiet {
		fmt.Printf("Cherry-picking commit %s onto %d projects...\n", commit, len(projects))
	}

	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("Cherry-picking in %s...\n", p.Name)
		}
		// Example: Run git cherry-pick command
		_, err := p.GitRepo.RunCommand("cherry-pick", commit)
		if err != nil {
			// Handle error appropriately (e.g., collect errors, print, fail fast)
			fmt.Printf("Error cherry-picking in %s: %v\n", p.Name, err)
			// Decide whether to continue or return error
		}
	}

	if !opts.Quiet {
		fmt.Println("Cherry-pick complete (potentially with errors).")
	}

	return nil // Adjust error handling as needed
}