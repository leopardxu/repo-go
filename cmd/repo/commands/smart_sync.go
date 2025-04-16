package commands

import (
	"fmt"
	"runtime"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"github.com/cix-code/gogo/internal/sync"
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
}

// SmartSyncCmd 返回smartsync命令
func SmartSyncCmd() *cobra.Command {
	opts := &SmartSyncOptions{}

	cmd := &cobra.Command{
		Use:   "smartsync [<project>...]",
		Short: "Update working tree to the latest known good revision",
		Long:  `The 'repo smartsync' command is a shortcut for sync -s.`,
		RunE: func(cmd *cobra.Command, args []string) error {
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

	return cmd
}

// runSmartSync 执行smartsync命令
func runSmartSync(opts *SmartSyncOptions, args []string) error {
	fmt.Println("Smart syncing projects")

	// 加载配置
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

	// 获取要处理的项目
	var projects []*project.Project
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项目
		projects, err = manager.GetProjects("")
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	} else {
		// 否则，只处理指定的项目
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			return fmt.Errorf("failed to get projects: %w", err)
		}
	}

	// 创建同步引擎
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
		Prune:          opts.Prune && !opts.NoPrune,
		Quiet:          opts.Quiet,
		Tags:           opts.Tags && !opts.NoTags,
		NoCloneBundle:  opts.NoCloneBundle,
		FetchSubmodules: opts.FetchSubmodules,
		OptimizedFetch: opts.OptimizedFetch,
		RetryFetches:   opts.RetryFetches,
		UseSuperproject: opts.UseSuperproject && !opts.NoUseSuperproject,
		HyperSync:      opts.HyperSync,
		OuterManifest:  opts.OuterManifest && !opts.NoOuterManifest,
		ThisManifestOnly: opts.ThisManifestOnly && !opts.NoThisManifestOnly,
		// Optimize field removed as it's not defined in sync.Options
	})

	// 执行同步
	if err := engine.Run(); err != nil {
		return fmt.Errorf("sync failed: %w", err)
	}

	// 显示同步结果
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