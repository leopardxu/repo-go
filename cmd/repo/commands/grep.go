package commands

import (
	"fmt"
	"os/exec" // <-- Add import for exec
	// "regexp" // Remove if not used
	"strings" // <-- Add import for strings

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// GrepOptions holds the options for the grep command
type GrepOptions struct {
	IgnoreCase   bool
	FixedStrings bool
	LineNumber   bool
	FilesWithMatches bool
	Quiet        bool
	Pattern      string
	Groups       string
	Config       *config.Config // <-- Add Config field
	CommonManifestOptions
}

// GrepCmd creates the grep command
func GrepCmd() *cobra.Command {
	opts := &GrepOptions{}
	cmd := &cobra.Command{
		Use:   "grep <pattern> [<project>...]",
		Short: "Print lines matching a pattern",
		Long:  `Looks for specified patterns in the working tree files of the specified projects.`,
		Args:  cobra.MinimumNArgs(1), // Requires at least the pattern
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load() // Load config
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			opts.Pattern = args[0]
			return runGrep(opts, args[1:]) // Pass project names
		},
	}

	// Add flags
	cmd.Flags().BoolVarP(&opts.IgnoreCase, "ignore-case", "i", false, "ignore case distinctions")
	cmd.Flags().BoolVarP(&opts.FixedStrings, "fixed-strings", "F", false, "interpret pattern as fixed string")
	cmd.Flags().BoolVarP(&opts.LineNumber, "line-number", "n", false, "prefix matching lines with line number")
	cmd.Flags().BoolVarP(&opts.FilesWithMatches, "files-with-matches", "l", false, "show only file names containing matches")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict execution to projects in specified groups (comma-separated)")
	AddManifestFlags(cmd, &opts.CommonManifestOptions) // Pass opts.CommonManifestOptions

	return cmd
}

// runGrep executes the grep command logic
func runGrep(opts *GrepOptions, projectNames []string) error {
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
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",") // Now strings is defined
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

	// Build git grep arguments
	grepArgs := []string{"grep"}
	if opts.IgnoreCase {
		grepArgs = append(grepArgs, "-i")
	}
	if opts.FixedStrings {
		grepArgs = append(grepArgs, "-F")
	}
	if opts.LineNumber {
		grepArgs = append(grepArgs, "-n")
	}
	if opts.FilesWithMatches {
		grepArgs = append(grepArgs, "-l")
	}
	grepArgs = append(grepArgs, "--color=always") // Assuming color is desired
	grepArgs = append(grepArgs, "-e", opts.Pattern)

	// Execute grep in each project
	if !opts.Quiet {
		fmt.Printf("Grepping for '%s' in %d projects...\n", opts.Pattern, len(projects))
	}

	var foundMatches bool
	for _, p := range projects {
		if p.Worktree == "" { // Skip projects without a worktree
			continue
		}
		// Run git grep within the project directory
		cmd := exec.Command("git", grepArgs...) // Now exec is defined
		cmd.Dir = p.Worktree
		output, err := cmd.CombinedOutput() // Capture combined stdout/stderr

		// git grep exits with 1 if no matches are found, 0 if matches are found, >1 on error
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				if exitErr.ExitCode() == 1 {
					// No matches found, not necessarily an error for grep
					continue
				}
			}
			// Actual error occurred
			fmt.Printf("Error grepping in %s: %v\nOutput:\n%s\n", p.Name, err, string(output))
			continue // Or handle error differently
		}

		// Matches found
		foundMatches = true
		if len(output) > 0 {
			// Prefix output with project name
			lines := strings.Split(strings.TrimSpace(string(output)), "\n") // Now strings is defined
			for _, line := range lines {
				fmt.Printf("%s:%s\n", p.Name, line)
			}
		}
	}

	if !foundMatches && !opts.Quiet {
		// fmt.Println("No matches found.") // Optional: Inform if no matches anywhere
	}

	return nil // Adjust error handling based on whether grep errors should fail the command
}