package commands

import (
	"fmt"
	"strings"

	"github.com/cix-code/gogo/internal/config" // Ensure config is imported
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/spf13/cobra"
)

// DiffOptions holds the options for the diff command
type DiffOptions struct {
	Quiet  bool
	Config *config.Config
	CommonManifestOptions
}

// 加载配置
func loadConfig() (*config.Config, error) {
	return config.Load()
}

// 解析清单
func loadManifest(cfg *config.Config) (*manifest.Manifest, error) {
	parser := manifest.NewParser()
	return parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
}

// 获取项目列表
func getProjects(manager *project.Manager, projectNames []string) ([]*project.Project, error) {
	if len(projectNames) == 0 {
		return manager.GetProjects(nil)
	}
	return manager.GetProjectsByNames(projectNames)
}

// 并发执行diff操作
type diffResult struct {
	Name string
	Output string
	Err   error
}

func runDiff(opts *DiffOptions, projectNames []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	mf, err := loadManifest(cfg)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	manager := project.NewManager(mf, cfg)
	projects, err := getProjects(manager, projectNames)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	if !opts.Quiet {
		fmt.Printf("Diff %d project(s)\n", len(projects))
	}

	maxConcurrency := 8
	sem := make(chan struct{}, maxConcurrency)
	results := make(chan diffResult, len(projects))

	for _, p := range projects {
		sem <- struct{}{}
		go func(proj *project.Project) {
			defer func() { <-sem }()
			outBytes, err := proj.GitRepo.RunCommand("diff")
			out := string(outBytes)
			results <- diffResult{Name: proj.Name, Output: out, Err: err}
		}(p)
	}

	for i := 0; i < len(projects); i++ {
		res := <-results
		if res.Err != nil {
			fmt.Printf("Error in %s: %v\n", res.Name, res.Err)
			continue
		}
		if res.Output != "" {
			fmt.Printf("--- %s ---\n%s\n", res.Name, res.Output)
		} else if !opts.Quiet {
			fmt.Printf("--- %s ---\n(No changes)\n", res.Name)
		}
	}

	return nil
}

// DiffCmd creates the diff command
func DiffCmd() *cobra.Command {
	opts := &DiffOptions{}
	cmd := &cobra.Command{
		Use:   "diff [<project>...]",
		Short: "Show changes between commit, working tree, etc",
		Long:  `Shows changes between the working tree and the index or a commit.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDiff(opts, args)
		},
	}

	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)
	return cmd
}