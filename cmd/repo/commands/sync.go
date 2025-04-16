package commands

import (
	"fmt"
	"runtime"
	"strings"
	
	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/sync"
	"github.com/spf13/cobra"
)

// SyncOptions 包含sync命令的选项
type SyncOptions struct {
	Jobs           int
	JobsNetwork    int
	JobsCheckout   int
	CurrentBranch  bool
	NoCurrentBranch bool
	Detach         bool
	ForceSync      bool
	ForceRemoveDirty bool
	ForceOverwrite bool
	LocalOnly      bool
	NetworkOnly    bool
	Prune          bool
	Quiet          bool
	SmartSync      bool
	Tags           bool
	NoCloneBundle  bool
	FetchSubmodules bool
	NoTags         bool
	OptimizedFetch bool
	RetryFetches   int
	Groups         string
	FailFast       bool
	NoManifestUpdate bool
	ManifestServerUsername string
	ManifestServerPassword string
	UseSuperproject bool
	NoUseSuperproject bool
	HyperSync      bool
	SmartTag       string
	OuterManifest  bool
	NoOuterManifest bool
	ThisManifestOnly bool
	NoThisManifestOnly bool
}

// SyncCmd 返回sync命令
func SyncCmd() *cobra.Command {
	opts := &SyncOptions{
		Jobs:         runtime.NumCPU() * 2,
		RetryFetches: 3,
	}

	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Update working tree to the latest revision",
		Long:  `Synchronize the local repository with the remote repositories.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of parallel jobs")
	cmd.Flags().IntVar(&opts.JobsNetwork, "jobs-network", opts.Jobs, "number of network jobs to run in parallel")
	cmd.Flags().IntVar(&opts.JobsCheckout, "jobs-checkout", opts.Jobs, "number of local checkout jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "fetch only current branch")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "fetch all branches from server")
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local changes")
	cmd.Flags().BoolVar(&opts.ForceRemoveDirty, "force-remove-dirty", false, "force remove projects with uncommitted modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force cleanup local uncommitted changes")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "only update working tree, don't fetch")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "no-manifest-update", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "nmu", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVarP(&opts.NetworkOnly, "network-only", "n", false, "fetch only, don't update working tree")
	cmd.Flags().BoolVarP(&opts.Prune, "prune", "p", false, "delete projects not in manifest")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "be quiet")
	cmd.Flags().BoolVarP(&opts.SmartSync, "smart-sync", "s", false, "smart sync using manifest from the latest known good build")
	cmd.Flags().BoolVarP(&opts.Tags, "tags", "t", false, "fetch tags")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.FetchSubmodules, "fetch-submodules", false, "fetch submodules")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", false, "don't fetch tags")
	cmd.Flags().BoolVar(&opts.OptimizedFetch, "optimized-fetch", false, "only fetch projects fixed to sha1 if revision does not exist locally")
	cmd.Flags().IntVar(&opts.RetryFetches, "retry-fetches", opts.RetryFetches, "number of times to retry fetches")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict to projects matching the specified groups")
	cmd.Flags().BoolVar(&opts.FailFast, "fail-fast", false, "stop syncing after first error is hit")
	cmd.Flags().BoolVar(&opts.UseSuperproject, "use-superproject", false, "use the manifest superproject to sync projects")
	cmd.Flags().BoolVar(&opts.NoUseSuperproject, "no-use-superproject", false, "disable use of manifest superprojects")
	cmd.Flags().BoolVar(&opts.HyperSync, "hyper-sync", false, "only update projects changed on git server")
	cmd.Flags().StringVar(&opts.SmartTag, "smart-tag", "", "smart sync using manifest from a known tag")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.NoThisManifestOnly, "all-manifests", false, "operate on this manifest and its submanifests")
	cmd.Flags().StringVarP(&opts.ManifestServerUsername, "manifest-server-username", "u", "", "username to authenticate with the manifest server")
	cmd.Flags().StringVarP(&opts.ManifestServerPassword, "manifest-server-password", "w", "", "password to authenticate with the manifest server")

	return cmd
}

// runSync 执行sync命令
func runSync(opts *SyncOptions, args []string) error {
	if !opts.Quiet {
		fmt.Println("Syncing repositories")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName)
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg)

	// 获取要同步的项目
	projects, err := manager.GetProjects(opts.Groups)
	if err != nil {
		return fmt.Errorf("failed to get projects: %w", err)
	}

	// 创建同步引擎
	// 处理组选项
	var groups []string
	if opts.Groups != "" {
		groups = strings.Split(opts.Groups, ",")
	}

	engine := sync.NewEngine(projects, &sync.Options{
		Jobs:           opts.Jobs,
		JobsNetwork:    opts.JobsNetwork,
		JobsCheckout:   opts.JobsCheckout,
		CurrentBranch:  opts.CurrentBranch && !opts.NoCurrentBranch,
		Detach:         opts.Detach,
		ForceSync:      opts.ForceSync,
		ForceRemoveDirty: opts.ForceRemoveDirty,
		ForceOverwrite: opts.ForceOverwrite,
		LocalOnly:      opts.LocalOnly,
		NetworkOnly:    opts.NetworkOnly,
		Prune:          opts.Prune,
		Quiet:          opts.Quiet,
		SmartSync:      opts.SmartSync,
		Tags:           opts.Tags && !opts.NoTags,
		NoCloneBundle:  opts.NoCloneBundle,
		FetchSubmodules: opts.FetchSubmodules,
		OptimizedFetch: opts.OptimizedFetch,
		RetryFetches:   opts.RetryFetches,
		Groups:         groups,
		FailFast:       opts.FailFast,
		NoManifestUpdate: opts.NoManifestUpdate,
		UseSuperproject: opts.UseSuperproject && !opts.NoUseSuperproject,
		HyperSync:      opts.HyperSync,
		SmartTag:       opts.SmartTag,
		OuterManifest:  opts.OuterManifest && !opts.NoOuterManifest,
		ThisManifestOnly: opts.ThisManifestOnly && !opts.NoThisManifestOnly,
	})

	// 执行同步
	if err := engine.Run(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	if !opts.Quiet {
		fmt.Println("Sync completed successfully")
	}
	return nil
}