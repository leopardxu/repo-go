package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/sync" // Ensure sync is imported
	"github.com/spf13/cobra"
)

// CheckoutOptions holds the options for the checkout command
type CheckoutOptions struct {
	Detach         bool
	ForceSync      bool
	ForceOverwrite bool
	JobsCheckout   int
	Quiet          bool
	Config         *config.Config // <-- Add Config field
	CommonManifestOptions
}

// CheckoutCmd creates the checkout command
func CheckoutCmd() *cobra.Command {
	opts := &CheckoutOptions{}
	cmd := &cobra.Command{
		Use:   "checkout <branch> [<project>...]",
		Short: "Checkout a branch for development",
		Long:  `Checks out a branch for development, creating it if necessary.`,
		Args:  cobra.MinimumNArgs(1), // Requires at least the branch name
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			// Pass branch name (args[0]) and optional project names (args[1:])
			return runCheckout(opts, args[0], args[1:])
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force overwrite existing files") // Assuming this flag exists
	cmd.Flags().IntVarP(&opts.JobsCheckout, "jobs-checkout", "j", 8, "number of projects to checkout in parallel")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runCheckout executes the checkout command logic
func runCheckout(opts *CheckoutOptions, branchName string, projectNames []string) error {
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

	// Create sync engine options from CheckoutOptions
	syncOpts := &sync.Options{
		JobsCheckout:   opts.JobsCheckout,
		Detach:         opts.Detach,
		ForceSync:      opts.ForceSync,
		ForceOverwrite: opts.ForceOverwrite,
		Quiet:          opts.Quiet,
		// Map other relevant fields if necessary
	}

	// Create sync engine - Corrected arguments and commented out unused variable
	// engine := sync.NewEngine(projects, syncOpts, manifest, opts.Config) // Pass arguments in correct order
	_ = syncOpts // Use syncOpts to avoid unused variable error if engine is commented out

	// Perform checkout operation (assuming engine has a Checkout method)
	// This part needs implementation based on how checkout should work
	fmt.Printf("Checking out branch '%s' for %d projects...\n", branchName, len(projects))
	// err = engine.CheckoutBranch(branchName, projects) // Example call using the engine
	// if err != nil {
	//     return fmt.Errorf("checkout failed: %w", err)
	// }

	if !opts.Quiet {
		fmt.Println("Checkout operation needs implementation in runCheckout.")
	}

	return nil // Placeholder return
}