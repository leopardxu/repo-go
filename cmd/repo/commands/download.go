package commands

import (
	"fmt"
	"strings"

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
			return runDownload(opts, args)
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}



// loadDownloadConfig loads the configuration
func loadDownloadConfig() (*config.Config, error) {
	return config.Load()
}

// loadDownloadManifest loads the manifest file
func loadDownloadManifest(cfg *config.Config) (*manifest.Manifest, error) {
	parser := manifest.NewParser()
	return parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
}

// 获取项目列表
func getDownloadProjects(manager *project.Manager, projectNames []string) ([]*project.Project, error) {
	if len(projectNames) == 0 {
		return manager.GetProjects(nil)
	}
	return manager.GetProjectsByNames(projectNames)
}

type downloadResult struct {
	Name   string
	Err    error
}

func runDownload(opts *DownloadOptions, projectNames []string) error {
	cfg, err := loadDownloadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	mf, err := loadDownloadManifest(cfg)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	manager := project.NewManager(mf, cfg)
	projects, err := getDownloadProjects(manager, projectNames)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	if !opts.Quiet {
		fmt.Printf("Downloading %d project(s)\n", len(projects))
	}

	maxConcurrency := 8
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan downloadResult, len(projects))

	for _, p := range projects {
		sem <- struct{}{}
		go func(proj *project.Project) {
			defer func() { <-sem }()
			_, err := proj.GitRepo.RunCommand("fetch", "--prune")
			results <- downloadResult{Name: proj.Name, Err: err}
		}(p)
	}

	hasErr := false
	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			fmt.Printf("Error downloading for %s: %v\n", res.Name, res.Err)
			hasErr = true
		} else if !opts.Quiet {
			fmt.Printf("Downloaded for %s\n", res.Name)
		}
	}

	if !opts.Quiet {
		fmt.Println("Download complete.")
	}

	if hasErr {
		return fmt.Errorf("some projects failed to download")
	}
	return nil
}