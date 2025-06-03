package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/leopardxu/repo-go/internal/repo_sync"
	"github.com/spf13/cobra"
)

// SyncOptions 包含sync命令的选项
type SyncOptions struct {
	Jobs                   int
	JobsNetwork            int
	JobsCheckout           int
	CurrentBranch          bool
	NoCurrentBranch        bool
	Detach                 bool
	ForceSync              bool
	ForceRemoveDirty       bool
	ForceOverwrite         bool
	LocalOnly              bool
	NetworkOnly            bool
	Prune                  bool
	Quiet                  bool
	Verbose                bool // 是否显示详细日志
	SmartSync              bool
	Tags                   bool
	NoCloneBundle          bool
	FetchSubmodules        bool
	NoTags                 bool
	OptimizedFetch         bool
	RetryFetches           int
	Groups                 string
	FailFast               bool
	NoManifestUpdate       bool
	ManifestServerUsername string
	ManifestServerPassword string
	UseSuperproject        bool
	NoUseSuperproject      bool
	HyperSync              bool
	SmartTag               string
	NoThisManifestOnly     bool
	GitLFS                 bool   // 是否启用Git LFS支持
	DefaultRemote          string // 默认远程仓库名称，用于解决分支匹配多个远程的问题
	Config                 *config.Config
	CommonManifestOptions
}

// syncStats 用于统计同步结果
type syncStats struct {
	mu      sync.Mutex
	total   int
	success int
	failed  int
	cloned  int
}

// increment 增加统计计数
func (s *syncStats) increment(success bool, isClone bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.total++
	if success {
		s.success++
		if isClone {
			s.cloned++
		}
	} else {
		s.failed++
	}
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

			// 如果设置了日志文件，配置日志输出
			logFile := os.Getenv("GOGO_LOG_FILE")
			if logFile != "" {
				if err := log.SetDebugFile(logFile); err != nil {
					fmt.Printf("警告: 无法设置日志文件 %s: %v\n", logFile, err)
				}
			}

			return runSync(opts, args, log)
		},
	}

	// 添加命令行选项
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", opts.Jobs, "number of parallel jobs (default: based on number of CPU cores)")
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
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.Verbose, "verbose", false, "show all output including debug logs")
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
	cmd.Flags().BoolVar(&opts.GitLFS, "git-lfs", true, "启用 Git LFS 支持")
	cmd.Flags().StringVar(&opts.DefaultRemote, "default-remote", "", "设置默认远程仓库名称，用于解决分支匹配多个远程的问题")

	return cmd
}

// runSync 执行sync命令
func runSync(opts *SyncOptions, args []string, log logger.Logger) error {
	// 创建统计对象
	stats := &syncStats{}

	// 加载配置
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}
	opts.Config = cfg

	// 检查manifest.xml 文件是否存在
	manifestPath := filepath.Join(cfg.RepoRoot, ".repo", "manifest.xml")
	if _, err := os.Stat(manifestPath); os.IsNotExist(err) {
		log.Error("manifest.xml文件不存在，请先运行 'repo init' 命令")
		return fmt.Errorf("manifest.xml文件不存在，请先运行 'repo init' 命令")
	}

	// 如果命令行没有指定groups 参数，则从配置文件中读取
	if opts.Groups == "" && cfg.Groups != "" {
		log.Debug("从配置文件中读取组信息 %s", cfg.Groups)
		opts.Groups = cfg.Groups
		log.Info("使用配置文件中的组信息 %s", cfg.Groups)
	}

	// 加载合并后的清单文件(.repo/manifest.xml)，不使用原始仓库列表
	log.Debug("正在加载合并后的清单文件: %s", manifestPath)
	parser := manifest.NewParser()
	var groupsSlice []string
	if opts.Groups != "" {
		groupsSlice = strings.Split(opts.Groups, ",")
		// 去除空白项
		validGroups := make([]string, 0, len(groupsSlice))
		for _, g := range groupsSlice {
			g = strings.TrimSpace(g)
			if g != "" {
				validGroups = append(validGroups, g)
			}
		}
		groupsSlice = validGroups
		log.Info("根据以下组过滤清单: %v", groupsSlice)
	} else {
		log.Info("未指定组过滤，将加载所有项目")
	}

	// 解析合并后的清单文件，根据组过滤项目
	manifestObj, err := parser.ParseFromFile(manifestPath, groupsSlice)
	if err != nil {
		log.Error("解析清单失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	log.Debug("成功加载清单，包含 %d 个项目", len(manifestObj.Projects))

	// 创建项目管理器
	log.Debug("正在初始化项目管理器...")
	manager := project.NewManagerFromManifest(manifestObj, opts.Config)

	var projects []*project.Project
	if len(args) == 0 {
		// 如果没有指定项目，则处理所有项
		log.Debug("获取所有项..")
		// 直接使用 groupsSlice 过滤项目，确保只获取指定组的项目
		if len(groupsSlice) > 0 {
			log.Debug("根据组过滤获取项 %v", groupsSlice)
			projects, err = manager.GetProjectsInGroups(groupsSlice)
		} else {
			log.Debug("获取所有项目，不进行组过滤")
			projects, err = manager.GetProjectsInGroups(nil)
		}
		if err != nil {
			log.Error("获取项目失败: %v", err)
			return fmt.Errorf("获取项目失败: %w", err)
		}
		log.Debug("共获取到 %d 个项目", len(projects))
	} else {
		// 否则，只处理指定的项目
		log.Debug("根据名称获取项目: %v", args)
		projects, err = manager.GetProjectsByNames(args)
		if err != nil {
			log.Error("根据名称获取项目失败: %v", err)
			return fmt.Errorf("根据名称获取项目失败: %w", err)
		}
		log.Debug("共获取到 %d 个项目", len(projects))
	}

	// 项目已经在GetProjectsInGroups 中根据组过滤，不需要再次过滤
	log.Info("找到 %d 个匹配项目", len(projects))

	// 如果过滤后没有项目，提前返回错误
	if len(projects) == 0 {
		log.Warn("在指定组 %v 中未找到匹配的项目，请检查组名是否正确", groupsSlice)
		return fmt.Errorf("在指定组 %v 中未找到匹配的项目", groupsSlice)
	}

	// 检查是否有项目需要同步
	if len(projects) == 0 {
		log.Warn("没有找到匹配的项目需要同步")
		return fmt.Errorf("没有找到匹配的项目需要同步")
	}

	// 创建同步引擎
	log.Debug("创建同步引擎...")
	// 使用已经处理好的 groupsSlice，避免重复处理
	if len(groupsSlice) > 0 {
		log.Info("使用以下组过滤项目 %v", groupsSlice)
	} else {
		log.Info("未指定组过滤，将同步所有项目")
	}

	engine := repo_sync.NewEngine(&repo_sync.Options{
		Jobs:                   opts.Jobs,
		JobsNetwork:            opts.JobsNetwork,
		JobsCheckout:           opts.JobsCheckout,
		CurrentBranch:          opts.CurrentBranch && !opts.NoCurrentBranch,
		Detach:                 opts.Detach,
		ForceSync:              opts.ForceSync,
		ForceRemoveDirty:       opts.ForceRemoveDirty,
		ForceOverwrite:         opts.ForceOverwrite,
		LocalOnly:              opts.LocalOnly,
		NetworkOnly:            opts.NetworkOnly,
		Prune:                  opts.Prune,
		Quiet:                  opts.Quiet,
		Verbose:                opts.Verbose,
		SmartSync:              opts.SmartSync,
		Tags:                   opts.Tags && !opts.NoTags,
		NoCloneBundle:          opts.NoCloneBundle,
		FetchSubmodules:        opts.FetchSubmodules,
		OptimizedFetch:         opts.OptimizedFetch,
		RetryFetches:           opts.RetryFetches,
		Groups:                 groupsSlice, // 传递已处理的分组信息，确保只克隆指定组的仓
		FailFast:               opts.FailFast,
		NoManifestUpdate:       opts.NoManifestUpdate,
		UseSuperproject:        opts.UseSuperproject && !opts.NoUseSuperproject,
		HyperSync:              opts.HyperSync,
		SmartTag:               opts.SmartTag,
		ManifestServerUsername: opts.ManifestServerUsername,
		ManifestServerPassword: opts.ManifestServerPassword,
		GitLFS:                 opts.GitLFS,        // 添加Git LFS支持选项
		DefaultRemote:          opts.DefaultRemote, // 添加默认远程仓库选项
		Config:                 opts.Config,        // 添加Config字段，传递配置信
	}, manifestObj, log)

	// 设置要同步的项目
	engine.SetProjects(projects)

	// 执行同步
	log.Info("开始同步项目，并行任务 %d...", opts.Jobs)
	err = engine.Sync()

	// 处理同步结果
	if err != nil {
		log.Error("同步操作完成，但有错 %v", err)
		stats.failed = len(projects) // 更新统计信息
		return err
	}

	// 更新统计信息
	stats.total = len(projects)
	stats.success = len(projects)
	log.Info("同步操作成功完成，共同步 %d 个项目", stats.total)
	return nil
}

// filterProjectsByGroups 根据组过滤项目
func filterProjectsByGroups(projects []*project.Project, groups []string) []*project.Project {
	if len(groups) == 0 {
		return projects
	}

	fmt.Printf("根据以下组过滤项 %v\n", groups)
	fmt.Printf("过滤前的项目数量: %d\n", len(projects))

	var filtered []*project.Project
	for _, p := range projects {
		if p.IsInAnyGroup(groups) {
			filtered = append(filtered, p)
		}
	}

	fmt.Printf("过滤后的项目数量: %d (原始数量: %d)\n", len(filtered), len(projects))
	if len(filtered) == 0 {
		fmt.Printf("警告: 过滤后没有匹配的项目，请检查组名是否正确\n")
	}
	return filtered
}
