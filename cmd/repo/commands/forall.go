package commands

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// ForallOptions holds the options for the forall command
type ForallOptions struct {
	Command     string
	Parallel    bool
	Jobs        int
	IgnoreErrors bool
	Quiet       bool
	Verbose     bool
	Groups      string
	Config      *config.Config // <-- Add Config field
	CommonManifestOptions
}

// ForallCmd creates the forall command
func ForallCmd() *cobra.Command {
	opts := &ForallOptions{}
	cmd := &cobra.Command{
		Use:   "forall [<project>...] -c <command> [<arg>...]",
		Short: "Run a shell command in each project",
		Long:  `Executes the same shell command in the working directory of each specified project.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config

			// Separate project names from the command and its arguments
			projectNames := args
			commandIndex := cmd.ArgsLenAtDash()
			if commandIndex != -1 {
				projectNames = args[:commandIndex]
				opts.Command = strings.Join(args[commandIndex:], " ")
			}

			if opts.Command == "" {
				return fmt.Errorf("command (-c) is required")
			}

			return runForall(opts, projectNames)
		},
	}

	// Add flags
	cmd.Flags().StringVarP(&opts.Command, "command", "c", "", "command and arguments to execute")
	cmd.Flags().BoolVarP(&opts.Parallel, "parallel", "p", false, "run commands in parallel")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel (if -p is specified)")
	cmd.Flags().BoolVar(&opts.IgnoreErrors, "ignore-errors", false, "continue executing even if a command fails")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show commands being executed")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict execution to projects in specified groups (comma-separated)")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runForall executes the forall command logic
func runForall(opts *ForallOptions, projectNames []string) error {
	// Load manifest
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName,strings.Split(opts.Config.Groups,","))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Create project manager
	manager := project.NewManager(manifest, opts.Config) // <-- Use opts.Config

	// Declare projects variable once
	var projects []*project.Project // <-- Declare projects variable here

	// Get projects to operate on
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",")
	}

	if len(projectNames) == 0 {
		projects, err = manager.GetProjects(groupsArg) // <-- Use groupsArg, assign with =
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// Filter specified projects by group if groups are provided
		filteredProjects, err := manager.GetProjectsByNames(projectNames)
		if err != nil {
			return fmt.Errorf("failed to get projects by name: %w", err)
		}
		if len(groupsArg) > 0 {
			for _, p := range filteredProjects {
				if p.IsInAnyGroup(groupsArg) {
					projects = append(projects, p)
				}
			}
		} else {
			projects = filteredProjects
		}
	}

	// Execute command in each project
	if !opts.Quiet {
		fmt.Printf("Executing command '%s' in %d projects...\n", opts.Command, len(projects))
	}

	type forallResult struct {
		Name string
		Err  error
	}

	maxConcurrency := opts.Jobs
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}

	sem := make(chan struct{}, maxConcurrency)
	results := make(chan forallResult, len(projects))

	for _, p := range projects {
		if p.Worktree == "" { // Skip projects without a worktree
			continue
		}

		sem <- struct{}{}
		go func(proj *project.Project) {
			defer func() { <-sem }()
			if opts.Verbose {
				fmt.Printf("[%s] Executing: %s\n", proj.Name, opts.Command)
			}
			cmd := exec.Command("sh", "-c", opts.Command)
			cmd.Dir = proj.Worktree
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			results <- forallResult{Name: proj.Name, Err: err}
		}(p)
	}

	var hasErr bool
	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			errMsg := fmt.Sprintf("Error in %s: %v", res.Name, res.Err)
			if !opts.Quiet {
				fmt.Println(errMsg)
			}
			hasErr = true
			if !opts.IgnoreErrors {
				return fmt.Errorf("command failed in project %s", res.Name)
			}
		} else if !opts.Quiet && opts.Verbose {
			fmt.Printf("[%s] Command executed successfully\n", res.Name)
		}
	}

	if hasErr {
		return fmt.Errorf("forall command failed in some projects")
	}

	if !opts.Quiet {
		fmt.Println("Command execution complete.")
	}

	return nil
}