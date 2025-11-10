package commands

import (
	"runtime"

	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/spf13/cobra"
)

// SmartSyncCmd 返回smartsync命令
// smartsync 是 sync -s 的快捷方式，继承 sync 命令的所有选项
// 这与 Google Repo 的实现完全一致
func SmartSyncCmd() *cobra.Command {
	// 创建 sync 命令的选项
	opts := &SyncOptions{
		Jobs:         runtime.NumCPU() * 2,
		RetryFetches: 3,
	}

	cmd := &cobra.Command{
		Use:   "smartsync [<project>...]",
		Short: "Update working tree to the latest known good revision",
		Long:  `The 'repo smartsync' command is a shortcut for sync -s.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 创建日志记录器
			log := logger.NewDefaultLogger()

			// 根据选项设置日志级别
			if opts.Quiet {
				log.SetLevel(logger.LogLevelError)
			} else if opts.Verbose {
				log.SetLevel(logger.LogLevelDebug)
			} else {
				log.SetLevel(logger.LogLevelInfo)
			}

			// 自动启用 SmartSync 模式（这是 smartsync 命令的核心特性）
			opts.SmartSync = true

			// 调用 sync 命令的执行逻辑
			return runSync(opts, args, log)
		},
	}

	// 添加所有 sync 命令的选项，除了 --smart-sync（因为已自动启用）
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of parallel jobs (default: based on number of CPU cores)")
	cmd.Flags().IntVar(&opts.JobsNetwork, "jobs-network", opts.Jobs, "number of network jobs to run in parallel")
	cmd.Flags().IntVar(&opts.JobsCheckout, "jobs-checkout", opts.Jobs, "number of local checkout jobs to run in parallel")
	cmd.Flags().BoolVarP(&opts.CurrentBranch, "current-branch", "c", true, "fetch only current branch")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "fetch all branches from server")
	cmd.Flags().BoolVarP(&opts.Detach, "detach", "d", false, "detach projects back to manifest revision")
	cmd.Flags().BoolVarP(&opts.ForceSync, "force-sync", "f", false, "overwrite local changes")
	cmd.Flags().BoolVar(&opts.ForceRemoveDirty, "force-remove-dirty", false, "force remove projects with uncommitted modifications")
	cmd.Flags().BoolVar(&opts.ForceOverwrite, "force-overwrite", false, "force cleanup local uncommitted changes")
	cmd.Flags().BoolVar(&opts.ForceBroken, "force-broken", false, "continue syncing other projects if a project sync fails")
	cmd.Flags().BoolVarP(&opts.LocalOnly, "local-only", "l", false, "only update working tree, don't fetch")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "no-manifest-update", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVar(&opts.NoManifestUpdate, "nmu", false, "use the existing manifest checkout as-is")
	cmd.Flags().BoolVarP(&opts.NetworkOnly, "network-only", "n", false, "fetch only, don't update working tree")
	cmd.Flags().BoolVarP(&opts.Prune, "prune", "p", false, "delete projects not in manifest")
	cmd.Flags().BoolVar(&opts.NoPrune, "no-prune", false, "do not delete projects not in manifest")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "show all output including debug logs")
	// 注意：不添加 --smart-sync 选项，因为 smartsync 命令自动启用此模式
	cmd.Flags().BoolVarP(&opts.Tags, "tags", "t", false, "fetch tags")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.FetchSubmodules, "fetch-submodules", false, "fetch submodules")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", false, "don't fetch tags")
	cmd.Flags().BoolVar(&opts.OptimizedFetch, "optimized-fetch", false, "only fetch projects fixed to sha1 if revision does not exist locally")
	cmd.Flags().IntVar(&opts.RetryFetches, "retry-fetches", opts.RetryFetches, "number of times to retry fetches")
	cmd.Flags().BoolVar(&opts.FailFast, "fail-fast", false, "stop syncing after first error is hit")
	cmd.Flags().BoolVar(&opts.UseSuperproject, "use-superproject", false, "use the manifest superproject to sync projects")
	cmd.Flags().BoolVar(&opts.NoUseSuperproject, "no-use-superproject", false, "disable use of manifest superprojects")
	cmd.Flags().StringVarP(&opts.ManifestServerUsername, "manifest-server-username", "u", "", "username to authenticate with the manifest server")
	cmd.Flags().StringVarP(&opts.ManifestServerPassword, "manifest-server-password", "P", "", "password to authenticate with the manifest server")
	cmd.Flags().BoolVar(&opts.OuterManifest, "outer-manifest", false, "operate starting at the outermost manifest")
	cmd.Flags().BoolVar(&opts.NoOuterManifest, "no-outer-manifest", false, "do not operate on outer manifests")
	cmd.Flags().BoolVar(&opts.ThisManifestOnly, "this-manifest-only", false, "only operate on this (sub)manifest")
	cmd.Flags().BoolVar(&opts.NoThisManifestOnly, "all-manifests", false, "operate on this manifest and its submanifests")

	return cmd
}
