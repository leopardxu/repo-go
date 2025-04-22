package commands

import (
	"fmt"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DownloadOptions holds the options for the download command
type DownloadOptions struct {
	// Add specific options for download if needed
	Quiet  bool
	Config *config.Config // <-- Add Config field
	CommonManifestOptions
}

// DownloadCmd creates the download command
func DownloadCmd() *cobra.Command {
	opts := &DownloadOptions{}
	cmd := &cobra.Command{
		Use:   "download [<project>...]",
		Short: "Download project changes from the remote server",
		Long:  `Downloads changes for the specified projects from their remote repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			return runDownload(opts, args)
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runDownload executes the download command logic
func runDownload(opts *DownloadOptions, projectNames []string) error {
	// Load manifest
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Create project manager
	manager := project.NewManager(manifest, opts.Config) // <-- Use opts.Config

	// Declare projects variable once
	var projects []*project.Project // <-- Declare projects variable here

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

	// Perform download operation (e.g., git fetch)
	if !opts.Quiet {
		fmt.Printf("Downloading changes for %d projects...\n", len(projects))
	}

	for _, p := range projects {
		if !opts.Quiet {
			fmt.Printf("Downloading for %s...\n", p.Name)
		}
		// Example: Run git fetch command
		_, err := p.GitRepo.RunCommand("fetch", "--prune") // Example fetch command
		if err != nil {
			// Handle error appropriately
			fmt.Printf("Error downloading for %s: %v\n", p.Name, err)
			// Decide whether to continue or return error
		}
	}

	if !opts.Quiet {
		fmt.Println("Download complete (potentially with errors).")
	}

	return nil // Adjust error handling as needed
}