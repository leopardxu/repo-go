package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DiffOptions holds the options for the diff command
type DiffOptions struct {
	// Add specific options for diff if needed
	Quiet  bool
	Config *config.Config // <-- Add Config field
	CommonManifestOptions
}

// DiffCmd creates the diff command
func DiffCmd() *cobra.Command {
	opts := &DiffOptions{}
	cmd := &cobra.Command{
		Use:   "diff [<project>...]",
		Short: "Show changes between commit, working tree, etc",
		Long:  `Shows changes between the working tree and the index or a commit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			return runDiff(opts, args)
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runDiff executes the diff command logic
func runDiff(opts *DiffOptions, projectNames []string) error {
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
		// Correct assignment mismatch
		projects, err = manager.GetProjectsByNames(projectNames) // <-- Assign both projects and err
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
	}

	// Perform diff operation
	if !opts.Quiet {
		fmt.Printf("Showing diff for %d projects...\n", len(projects))
	}

	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("--- Diff for %s ---\n", p.Name)
		}
		// Example: Run git diff command
		diffOutput, err := p.GitRepo.RunCommand("diff")
		if err != nil {
			// Handle error appropriately
			fmt.Printf("Error running diff in %s: %v\n", p.Name, err)
			continue // Or return error
		}
		if diffOutput != "" {
			fmt.Println(diffOutput)
		} else if !opts.Quiet {
			fmt.Println("(No changes)")
		}
	}

	return nil // Adjust error handling as needed
}