package commands

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/repo_sync"
	"github.com/spf13/cobra"
)

// SmartSyncOptions 包含smartsync命令的选项
type SmartSyncOptions struct {
	Detach                 bool
	Jobs                   int
	JobsNetwork            int
	JobsCheckout           int
	LocalOnly              bool
	NoTags                 bool
	ForceSync              bool
	ForceRemoveDirty       bool
	ForceOverwrite         bool
	Optimize               bool
	Quiet                  bool
	RetryFetches           int
	CurrentBranch          bool
	NoCurrentBranch        bool
	NoManifestUpdate       bool
	NetworkOnly            bool
	Prune                  bool
	Tags                   bool
	NoCloneBundle          bool
	FetchSubmodules        bool
	OptimizedFetch         bool
	UseSuperproject        bool
	NoUseSuperproject      bool
	HyperSync              bool
	OuterManifest          bool
	NoOuterManifest        bool
	ThisManifestOnly       bool
	NoThisManifestOnly     bool
	NoPrune               bool
	ManifestServerUsername string
	ManifestServerPassword string
	Config                 *config.Config // <-- Add Config field
	CommonManifestOptions                 // <-- Assuming CommonManifestOptions is needed if ManifestName is used indirectly
}

// SmartSyncCmd 返回smartsync命令
func SmartSyncCmd() *cobra.Command {
	opts := &SmartSyncOptions{}

	cmd := &cobra.Command{
		Use:   "smartsync [<project>...]",
		Short: "Update working tree to the latest known good revision",
		Long:  `The 'repo smartsync' command is a shortcut for sync -s.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Load config here as it's needed by runSmartSync
			cfg, err := config.Load()
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			opts.Config = cfg // Assign loaded config
			// Pass CommonManifestOptions if needed by AddManifestFlags
			// Ensure ManifestName is populated if used by config.Load or parser.ParseFromFile
			// If ManifestName comes from flags, it should be part of CommonManifestOptions
			// and AddManifestFlags should be called below.
			return runSmartSync(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", runtime.NumCPU()*2, "number of jobs to run in parallel (default: 8; based on number of CPU cores)")
	cmd.Flags().IntVar(&opts.JobsNetwork, "jobs-network", opts.Jobs, "number of network jobs to run in parallel (defaults to --jobs)")
	cmd.Flags().IntVar(&opts.JobsCheckout, "jobs-checkout", opts.Jobs, "number of local checkout jobs to run in parallel (defaults to --jobs)")
	cmd.Flags().BoolVar(&opts.ForceSync, "force-sync", false, "overwrite an existing git directory if it needs to point to a different object directory. WARNING: this may cause loss of data")
	cmd.Flags().BoolVar(&opts.ForceRemoveDirty, "force-remove-dirty", false, "force remove projects with uncommitted modifications if projects no longer exist in the manifest. WARNING: this may cause loss of data")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "DANGOUR: DO NOT USE UNLESS YOU KNOW WHAT YOUR ARE DOING,force cleanup local uncomitted changes")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "only update working tree, don't fetch")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "no-manifest-update", false, "use the existing manifest checkout as-is. (do not update to the latest revision)")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "nmu", false, "use the existing manifest checkout as-is. (do not update to the latest revision)")
	cmd.Flags().BoolVarP(&opts.NetworkOnly, "network-only", "n", false, "fetch only, don't update working tree")
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", false, "fetch only current branch from server")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "fetch all branches from server")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.FetchSubmodules, "fetch-submodules", false, "fetch submodules from server")
	cmd.Flags().BoolVar(&opts.UseSuperproject, "use-superproject", false, "use the manifest superproject to sync projects; implies -c")
	cmd.Flags().BoolVar(&opts.NoUseSuperproject, "no-use-superproject", false, "disable use of manifest superprojects")
	cmd.Flags().BoolVar(&opts.Tags, "tags", false, "fetch tags")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", true, "don't fetch tags (default)")
	cmd.Flags().BoolVar(&opts.OptimizedFetch, "optimized-fetch", false, "only fetch projects fixed to sha1 if revision does not exist locally")
	cmd.Flags().IntVar(&opts.RetryFetches, "retry-fetches", 3, "number of times to retry fetches on transient errors")
	cmd.Flags().BoolVar(&opts.Prune, "prune", true, "delete refs that no longer exist on the remote (default)")
	cmd.Flags().BoolVar(&opts.NoPrune, "no-prune", false, "do not delete refs that no longer exist on the remote")
	cmd.Flags().BoolVar(&opts.HyperSync, "hyper-sync", false, "only update projects changed on git server by checking against CIX Manifest Service")
	cmd.Flags().StringVarP(&opts.ManifestServerUsername, "manifest-server-username", "u", "", "username to authenticate with the manifest server")
	cmd.Flags().StringVarP(&opts.ManifestServerPassword, "manifest-server-password", "p", "", "password to authenticate with the manifest server")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.NoThisManifestOnly, "all-manifests", false, "operate on this manifest and its submanifests")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVarP(&opts.Optimize, "optimize", "o", true, "optimize sync strategy based on project status")
	// Add manifest flags if ManifestName is needed and comes from flags
	// AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runSmartSync 执行smartsync命令
func runSmartSync(opts *SmartSyncOptions, args []string) error {
	if !opts.Quiet {
		fmt.Println("Smart syncing projects")
	}

	// Config is now loaded in RunE and passed via opts
	cfg := opts.Config
	if cfg == nil {
		return fmt.Errorf("config not loaded")
	}

	// 加载清单
	parser := manifest.NewParser()
	manifest, err := parser.ParseFromFile(cfg.ManifestName,strings.Split(cfg.Groups,","))
	if err != nil {
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	// 创建项目管理器
	manager := project.NewManager(manifest, cfg)

	// 获取要处理的项目
	var projects []*project.Project
	if len(args) == 0 {
		projects, err = manager.GetProjects(nil)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 创建同步选项
	syncOpts := &repo_sync.Options{
		Jobs:             opts.Jobs,
		JobsNetwork:      opts.JobsNetwork,
		JobsCheckout:     opts.JobsCheckout,
		Detach:           opts.Detach,
		ForceSync:        opts.ForceSync,
		ForceRemoveDirty: opts.ForceRemoveDirty,
		ForceOverwrite:   opts.ForceOverwrite,
		LocalOnly:        opts.LocalOnly,
		NetworkOnly:      opts.NetworkOnly,
		Quiet:            opts.Quiet,
		CurrentBranch:    opts.CurrentBranch,
		NoTags:           opts.NoTags,
		Prune:            opts.Prune,
		OptimizedFetch:    opts.OptimizedFetch,
		UseSuperproject:  opts.UseSuperproject,
	}

	// 创建同步引擎
	engine := repo_sync.NewEngine(projects, syncOpts, manifest, cfg)

	// 使用单独的goroutine池处理网络和本地操作
	if err := engine.Run(); err != nil {
		if !opts.Quiet {
			fmt.Printf("\nSync completed with %d errors\n", len(engine.Errors()))
		}
		return fmt.Errorf("sync failed: %w", err)
	}

	if !opts.Quiet {
		fmt.Println("\nSync completed successfully")
	}

	return nil
}

// 删除或修改displaySmartSyncResults函数，因为我们不再使用SmartSync方法
// 如果需要保留，可以修改为显示简单的成功消息
func displaySmartSyncResults(results map[string]string) {
	fmt.Println("\nSync results:")
	
	for project, result := range results {
		fmt.Printf("%s: %s\n", project, result)
	}
}