package commands

import (
	"fmt"
	"sync"
	"os/exec" 
	"strings"

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
	manifest, err := parser.ParseFromFile(opts.Config.ManifestName,strings.Split(opts.Config.Groups,","))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// Create project manager
	manager := project.NewManager(manifest, opts.Config)

	// Declare projects variable once
	var projects []*project.Project

	// Get projects to operate on
	var groupsArg []string
	if opts.Groups != "" {
		groupsArg = strings.Split(opts.Groups, ",")
	}

	if len(projectNames) == 0 {
		projects, err = manager.GetProjects(groupsArg)
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
	grepArgs = append(grepArgs, "--color=always")
	grepArgs = append(grepArgs, "-e", opts.Pattern)

	// Execute grep in each project concurrently
	if !opts.Quiet {
		fmt.Printf("Grepping for '%s' in %d projects...\n", opts.Pattern, len(projects))
	}

	type grepResult struct {
		project *project.Project
		output []byte
		err    error
	}

	// Create worker pool
	maxWorkers := 8
	sem := make(chan struct{}, maxWorkers)
	results := make(chan grepResult, len(projects))
	var wg sync.WaitGroup

	for _, p := range projects {
		if p.Worktree == "" {
			continue
		}

		wg.Add(1)
		go func(p *project.Project) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			cmd := exec.Command("git", grepArgs...)
			cmd.Dir = p.Worktree
			output, err := cmd.CombinedOutput()
			results <- grepResult{p, output, err}
		}(p)
	}

	// Close results channel when all workers are done
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results
	var foundMatches bool
	var errors []error

	for res := range results {
		if res.err != nil {
			if exitErr, ok := res.err.(*exec.ExitError); ok {
				if exitErr.ExitCode() == 1 {
					continue
				}
			}
			errors = append(errors, fmt.Errorf("error grepping in %s: %v\nOutput:\n%s", 
					res.project.Name, res.err, string(res.output)))
			continue
		}

		if len(res.output) > 0 {
			foundMatches = true
			lines := strings.Split(strings.TrimSpace(string(res.output)), "\n")
			for _, line := range lines {
				fmt.Printf("%s:%s\n", res.project.Name, line)
			}
		}
	}

	if len(errors) > 0 {
		for _, err := range errors {
			fmt.Println(err)
		}
	}

	if !foundMatches && !opts.Quiet {
		// fmt.Println("No matches found.")
	}

	return nil
}