package commands

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/leopardxu/repo-go/internal/git"
	"github.com/leopardxu/repo-go/internal/hook"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/spf13/cobra"
)

// RepoConfig 表示repo配置
type RepoConfig struct {
	ManifestURL         string `json:"manifest_url"`
	ManifestBranch      string `json:"manifest_branch"`
	ManifestName        string `json:"manifest_name"`
	Groups              string `json:"groups"`
	Platform            string `json:"platform"`
	Mirror              bool   `json:"mirror"`
	Archive             bool   `json:"archive"`
	Worktree            bool   `json:"worktree"`
	Reference           string `json:"reference"`
	NoSmartCache        bool   `json:"no_smart_cache"`
	Dissociate          bool   `json:"dissociate"`
	Depth               int    `json:"depth"`
	PartialClone        bool   `json:"partial_clone"`
	PartialCloneExclude string `json:"partial_clone_exclude"`
	CloneFilter         string `json:"clone_filter"`
	UseSuperproject     bool   `json:"use_superproject"`
	CloneBundle         bool   `json:"clone_bundle"`
	GitLFS              bool   `json:"git_lfs"`
	RepoURL             string `json:"repo_url"`
	RepoRev             string `json:"repo_rev"`
	NoRepoVerify        bool   `json:"no_repo_verify"`
	StandaloneManifest  bool   `json:"standalone_manifest"`
	Submodules          bool   `json:"submodules"`
	CurrentBranch       bool   `json:"current_branch"`
	Tags                bool   `json:"tags"`
}

// InitOptions 包含init命令的选项
type InitOptions struct {
	CommonManifestOptions
	Verbose             bool
	Quiet               bool
	Debug               bool
	ManifestURL         string
	ManifestBranch      string
	ManifestName        string
	Groups              string
	Platform            string
	Submodules          bool
	StandaloneManifest  bool
	CurrentBranch       bool
	NoCurrentBranch     bool
	Tags                bool
	NoTags              bool
	Mirror              bool
	Archive             bool
	Worktree            bool
	Reference           string
	NoSmartCache        bool
	Dissociate          bool
	Depth               int
	PartialClone        bool
	NoPartialClone      bool
	PartialCloneExclude string
	CloneFilter         string
	UseSuperproject     bool
	NoUseSuperproject   bool
	CloneBundle         bool
	NoCloneBundle       bool
	GitLFS              bool
	NoGitLFS            bool
	RepoURL             string
	RepoRev             string
	NoRepoVerify        bool
	ConfigName          bool
}

// InitCmd 返回init命令
func InitCmd() *cobra.Command {
	opts := &InitOptions{}

	cmd := &cobra.Command{
		Use:   "init [options] [manifest url]",
		Short: "Initialize a repo client checkout in the current directory",
		Long:  `Initialize a repository client checkout in the current directory.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				opts.ManifestURL = args[0]
			}
			return runInit(opts)
		},
	}

	// 日志选项
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().BoolVar(&opts.Debug, "debug", false, "show debug output")

	// 清单选项
	cmd.Flags().StringVarP(&opts.ManifestURL, "manifest-url", "u", "", "manifest repository location")
	cmd.Flags().StringVarP(&opts.ManifestBranch, "manifest-branch", "b", "", "manifest branch or revision (use HEAD for default)")
	cmd.Flags().StringVarP(&opts.ManifestName, "manifest-name", "m", "default.xml", "initial manifest file")
	cmd.Flags().StringVarP(&opts.Groups, "groups", "g", "", "restrict manifest projects to ones with specified group(s)")
	// 修改这里，将 -p 改为 -P 或其他未使用的短标志
	cmd.Flags().StringVarP(&opts.Platform, "platform", "P", "", "restrict manifest projects to ones with a specified platform group")
	cmd.Flags().BoolVar(&opts.Submodules, "submodules", false, "sync any submodules associated with the manifest repo")
	cmd.Flags().BoolVar(&opts.StandaloneManifest, "standalone-manifest", false, "download the manifest as a static file")

	// 清单检出选项
	cmd.Flags().BoolVar(&opts.CurrentBranch, "current-branch", true, "fetch only current manifest branch")
	cmd.Flags().BoolVar(&opts.NoCurrentBranch, "no-current-branch", false, "fetch all manifest branches")
	cmd.Flags().BoolVar(&opts.Tags, "tags", false, "fetch tags in the manifest")
	cmd.Flags().BoolVar(&opts.NoTags, "no-tags", false, "don't fetch tags in the manifest")

	// 检出模式
	cmd.Flags().BoolVar(&opts.Mirror, "mirror", false, "create a replica of the remote repositories")
	cmd.Flags().BoolVar(&opts.Archive, "archive", false, "checkout an archive instead of a git repository")
	cmd.Flags().BoolVar(&opts.Worktree, "worktree", false, "use git-worktree to manage projects")

	// 项目检出优先
	cmd.Flags().StringVar(&opts.Reference, "reference", "", "location of mirror directory")
	cmd.Flags().BoolVar(&opts.NoSmartCache, "no-smart-cache", false, "disable CIX smart cache feature")
	cmd.Flags().BoolVar(&opts.Dissociate, "dissociate", false, "dissociate from reference mirrors after clone")
	cmd.Flags().IntVar(&opts.Depth, "depth", 1, "create a shallow clone with given depth")
	cmd.Flags().BoolVar(&opts.PartialClone, "partial-clone", false, "perform partial clone")
	cmd.Flags().BoolVar(&opts.NoPartialClone, "no-partial-clone", false, "disable use of partial clone")
	cmd.Flags().StringVar(&opts.PartialCloneExclude, "partial-clone-exclude", "", "exclude projects from partial clone")
	cmd.Flags().StringVar(&opts.CloneFilter, "clone-filter", "blob:none", "filter for use with --partial-clone")
	cmd.Flags().BoolVar(&opts.UseSuperproject, "use-superproject", false, "use the manifest superproject to sync projects")
	cmd.Flags().BoolVar(&opts.NoUseSuperproject, "no-use-superproject", false, "disable use of manifest superprojects")
	cmd.Flags().BoolVar(&opts.CloneBundle, "clone-bundle", false, "enable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.GitLFS, "git-lfs", false, "enable Git LFS support")
	cmd.Flags().BoolVar(&opts.NoGitLFS, "no-git-lfs", false, "disable Git LFS support")

	// repo版本选项
	cmd.Flags().StringVar(&opts.RepoURL, "repo-url", "", "repo repository location")
	cmd.Flags().StringVar(&opts.RepoRev, "repo-rev", "", "repo branch or revision")
	cmd.Flags().BoolVar(&opts.NoRepoVerify, "no-repo-verify", false, "do not verify repo source code")

	// 其他选项
	cmd.Flags().BoolVar(&opts.ConfigName, "config-name", false, "Always prompt for name/e-mail")

	// 多清单选项
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// saveRepoConfig 保存repo配置
func saveRepoConfig(cfg *RepoConfig) error {
	// 确保.repo目录存在
	if err := os.MkdirAll(".repo", 0755); err != nil {
		return fmt.Errorf("failed to create .repo directory: %w", err)
	}

	// 序列化配置
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to serialize config: %w", err)
	}

	// 写入配置文件
	configPath := filepath.Join(".repo", "config.json")
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// loadGitConfig 加载Git配置
func loadGitConfig() error {
	// 检查Git是否安装
	gitRunner := git.NewRunner()
	if _, err := gitRunner.Run("--version"); err != nil {
		return fmt.Errorf("git not found: %w", err)
	}

	// 检查Git配置
	output, err := gitRunner.Run("config", "--get", "user.name")
	if err != nil {
		return fmt.Errorf("failed to get user name: %w", err)
	}
	userName := strings.TrimSpace(string(output)) // 添加 string() 转换

	output, err = gitRunner.Run("config", "--get", "user.email")
	if err != nil {
		return fmt.Errorf("failed to get user email: %w", err)
	}
	userEmail := strings.TrimSpace(string(output)) // 添加 string() 转换

	// 使用userName和userEmail变量
	fmt.Printf("Using user: %s <%s>\n", userName, userEmail)
	if err != nil || strings.TrimSpace(string(output)) == "" {
		return fmt.Errorf("git user.email not set, please run 'git config --global user.email \"your.email@example.com\"'")
	}

	return nil
}

// promptForUserInfo 提示用户输入信息
func promptForUserInfo() error {
	gitRunner := git.NewRunner()

	// 检查用户名
	output, _ := gitRunner.Run("config", "--get", "user.name")
	if strings.TrimSpace(string(output)) == "" { // 添加string()转换
		fmt.Print("Enter your name: ")
		var name string
		fmt.Scanln(&name)
		if name != "" {
			if _, err := gitRunner.Run("config", "--global", "user.name", name); err != nil {
				return fmt.Errorf("failed to set git user.name: %w", err)
			}
		}
	}

	// 检查邮箱
	output, _ = gitRunner.Run("config", "--get", "user.email")
	if strings.TrimSpace(string(output)) == "" { // 添加string()转换
		fmt.Print("Enter your email: ")
		var email string
		fmt.Scanln(&email)
		if email != "" {
			if _, err := gitRunner.Run("config", "--global", "user.email", email); err != nil {
				return fmt.Errorf("failed to set git user.email: %w", err)
			}
		}
	}

	return nil
}

// cloneManifestRepo 克隆清单仓库
func cloneManifestRepo(gitRunner git.Runner, cfg *RepoConfig) error {
	// 创建.repo/manifests目录
	manifestsDir := filepath.Join(".repo", "manifests")

	// 检查目录是否已存在
	if info, err := os.Stat(manifestsDir); err == nil && info.IsDir() {
		// 目录已存在，检查是否为空
		files, err := os.ReadDir(manifestsDir)
		if err != nil {
			return fmt.Errorf("failed to read manifests directory: %w", err)
		}

		// 如果目录不为空，需要先清空目录或更新现有仓库
		if len(files) > 0 {
			// 检查是否是有效的git仓库
			gitDirPath := filepath.Join(manifestsDir, ".git")
			if _, err := os.Stat(gitDirPath); err == nil {
				// 是有效的git仓库，执行fetch更新
				currentDir, err := os.Getwd()
				if err != nil {
					return fmt.Errorf("failed to get current directory: %w", err)
				}

				// 切换到manifests目录
				if err := os.Chdir(manifestsDir); err != nil {
					return fmt.Errorf("failed to change to manifests directory: %w", err)
				}

				// 执行git fetch更新
				_, err = gitRunner.Run("fetch", "--all")

				// 如果指定了分支，切换到该分支
				if cfg.ManifestBranch != "" {
					// 先检查分支是否存在
					output, err := gitRunner.Run("branch", "-a")
					if err != nil {
						// 返回原目录
						if chErr := os.Chdir(currentDir); chErr != nil {
							return fmt.Errorf("failed to return to original directory: %w", chErr)
						}
						return fmt.Errorf("failed to list branches: %w", err)
					}

					// 检查分支是否存在
					branchExists := false
					remoteBranchExists := false
					remoteBranchName := "remotes/origin/" + cfg.ManifestBranch
					for _, line := range strings.Split(string(output), "\n") {
						if strings.Contains(line, " "+cfg.ManifestBranch) || strings.Contains(line, "* "+cfg.ManifestBranch) {
							branchExists = true
						}
						if strings.Contains(line, remoteBranchName) {
							remoteBranchExists = true
						}
						if branchExists && remoteBranchExists {
							break
						}
					}

					if branchExists {
						// 本地分支存在，切换到该分支
						_, err = gitRunner.Run("checkout", cfg.ManifestBranch)
					} else if remoteBranchExists {
						// 远程分支存在，从远程创建本地分支
						_, err = gitRunner.Run("checkout", "-b", cfg.ManifestBranch, "origin/"+cfg.ManifestBranch)
					} else {
						// 分支不存在，尝试从远程获取
						_, err = gitRunner.Run("fetch", "origin", cfg.ManifestBranch+":"+cfg.ManifestBranch)
						if err == nil {
							_, err = gitRunner.Run("checkout", cfg.ManifestBranch)
						} else {
							// 如果远程分支不存在，记录警告但继续使用当前分支
							log.Printf("警告: 无法获取指定的分支 %s，将使用当前分支", cfg.ManifestBranch)
							err = nil
						}
					}

					if err != nil {
						// 返回原目录
						if chErr := os.Chdir(currentDir); chErr != nil {
							return fmt.Errorf("failed to return to original directory: %w", chErr)
						}
						return fmt.Errorf("failed to checkout branch %s: %w", cfg.ManifestBranch, err)
					}
				}

				// 返回原目录
				if chErr := os.Chdir(currentDir); chErr != nil {
					return fmt.Errorf("failed to return to original directory: %w", chErr)
				}

				if err != nil {
					return fmt.Errorf("failed to update manifest repository: %w", err)
				}

				// 更新成功，直接返回
				return nil
			} else {
				// 不是有效的git仓库，需要清空目录
				if err := os.RemoveAll(manifestsDir); err != nil {
					return fmt.Errorf("failed to clean manifests directory: %w", err)
				}

				// 重新创建目录
				if err := os.MkdirAll(manifestsDir, 0755); err != nil {
					return fmt.Errorf("failed to create manifests directory: %w", err)
				}
			}
		}
	} else if os.IsNotExist(err) {
		// 目录不存在，创建它
		if err := os.MkdirAll(manifestsDir, 0755); err != nil {
			return fmt.Errorf("failed to create manifests directory: %w", err)
		}
	}

	// 构建克隆命令
	args := []string{"clone"}

	// 添加深度参数
	if cfg.Depth > 0 {
		args = append(args, fmt.Sprintf("--depth=%d", cfg.Depth))
	}

	// 添加分支参数
	if cfg.ManifestBranch != "" {
		args = append(args, "-b", cfg.ManifestBranch)
	}

	// 添加镜像参数
	if cfg.Mirror {
		args = append(args, "--mirror")
	}

	// 添加引用参数
	if cfg.Reference != "" {
		args = append(args, fmt.Sprintf("--reference=%s", cfg.Reference))
	}

	// 添加部分克隆参数
	if cfg.PartialClone {
		args = append(args, "--filter="+cfg.CloneFilter)
	}

	// 添加URL和目标目录
	args = append(args, cfg.ManifestURL, manifestsDir)

	// 使用goroutine池执行克隆命令
	errChan := make(chan error, 1)
	go func() {
		var lastErr error
		for i := 0; i < 3; i++ {
			_, err := gitRunner.Run(args...)
			if err == nil {
				errChan <- nil
				return
			}
			lastErr = err
			if strings.Contains(err.Error(), "Permission denied (publickey)") {
				errChan <- fmt.Errorf("SSH authentication failed: please ensure your SSH key is properly configured and added to the git server\nOriginal error: %w", err)
				return
			}
			if i < 2 {
				time.Sleep(time.Second * 2)
			}
		}
		if strings.Contains(lastErr.Error(), "fatal: repository '") {
			errChan <- fmt.Errorf("清单仓库URL无效或无法访问 %s\n请检查URL是否正确且网络可访问", lastErr)
		} else if strings.Contains(lastErr.Error(), "Could not read from remote repository") {
			errChan <- fmt.Errorf("无法从远程仓库读取 %s\n请检查权限和网络连接", lastErr)
		} else {
			errChan <- fmt.Errorf("克隆清单仓库失败: %s\n尝试次数: %d/3", lastErr, 3)
		}
	}()

	// 等待克隆完成或超时
	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("克隆清单仓库超时\n请检查网络连接或尝试增加超时时间")
	}

	// 如果需要子模块
	if cfg.Submodules {
		// 切换到manifests目录
		currentDir, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("failed to get current directory: %w", err)
		}

		if err := os.Chdir(manifestsDir); err != nil {
			return fmt.Errorf("failed to change to manifests directory: %w", err)
		}

		// 初始化子模块
		if _, err := gitRunner.Run("submodule", "update", "--init", "--recursive"); err != nil {
			if err := os.Chdir(currentDir); err != nil { // 确保返回原目录
				return fmt.Errorf("failed to return to original directory: %w", err)
			}
			return fmt.Errorf("failed to initialize submodules: %w", err)
		}

		// 返回原目录
		if err := os.Chdir(currentDir); err != nil {
			return fmt.Errorf("failed to return to original directory: %w", err)
		}
	}

	return nil
}

// validateOptions 验证选项冲突
func validateOptions(opts *InitOptions) error {
	if opts.CurrentBranch && opts.NoCurrentBranch {
		return fmt.Errorf("cannot specify both --current-branch and --no-current-branch")
	}
	if opts.Tags && opts.NoTags {
		return fmt.Errorf("cannot specify both --tags and --no-tags")
	}
	if opts.PartialClone && opts.NoPartialClone {
		return fmt.Errorf("cannot specify both --partial-clone and --no-partial-clone")
	}
	if opts.UseSuperproject && opts.NoUseSuperproject {
		return fmt.Errorf("cannot specify both --use-superproject and --no-use-superproject")
	}
	if opts.CloneBundle && opts.NoCloneBundle {
		return fmt.Errorf("cannot specify both --clone-bundle and --no-clone-bundle")
	}
	if opts.GitLFS && opts.NoGitLFS {
		return fmt.Errorf("cannot specify both --git-lfs and --no-git-lfs")
	}
	if opts.OuterManifest && opts.NoOuterManifest {
		return fmt.Errorf("cannot specify both --outer-manifest and --no-outer-manifest")
	}
	return nil
}

// runInit 执行init命令
func runInit(opts *InitOptions) error {
	// 创建日志记录
	log := logger.NewDefaultLogger()
	if opts.Debug {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Verbose {
		log.SetLevel(logger.LogLevelInfo)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
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

	log.Info("初始化repo 客户端..")

	// 验证选项冲突
	if err := validateOptions(opts); err != nil {
		log.Error("选项验证失败: %v", err)
		return err
	}

	// 创建配置
	cfg := &RepoConfig{
		ManifestURL:         opts.ManifestURL,
		ManifestBranch:      opts.ManifestBranch,
		ManifestName:        opts.ManifestName,
		Groups:              opts.Groups,
		Platform:            opts.Platform,
		Mirror:              opts.Mirror,
		Archive:             opts.Archive,
		Worktree:            opts.Worktree,
		Reference:           opts.Reference,
		NoSmartCache:        opts.NoSmartCache,
		Dissociate:          opts.Dissociate,
		Depth:               opts.Depth,
		PartialClone:        opts.PartialClone,
		PartialCloneExclude: opts.PartialCloneExclude,
		CloneFilter:         opts.CloneFilter,
		UseSuperproject:     opts.UseSuperproject,
		CloneBundle:         opts.CloneBundle,
		GitLFS:              opts.GitLFS,
		RepoURL:             opts.RepoURL,
		RepoRev:             opts.RepoRev,
		NoRepoVerify:        opts.NoRepoVerify,
		StandaloneManifest:  opts.StandaloneManifest,
		Submodules:          opts.Submodules,
		CurrentBranch:       opts.CurrentBranch,
		Tags:                opts.Tags,
	}

	// 处理配置名称提示
	if opts.ConfigName {
		log.Debug("提示用户输入 Git 用户信息")
		if err := promptForUserInfo(); err != nil {
			log.Error("提示用户信息失败: %v", err)
			return fmt.Errorf("failed to prompt for user info: %w", err)
		}
	} else {
		// 只检查Git是否安装，不强制要求配置用户信息
		log.Debug("检查 Git 是否已安装")
		gitRunner := git.NewRunner()
		if _, err := gitRunner.Run("--version"); err != nil {
			log.Error("Git 未安装 %v", err)
			return fmt.Errorf("git not found: %w", err)
		}
	}

	// 配置 Git 运行
	gitRunner := git.NewRunner()
	if opts.Debug {
		gitRunner.SetVerbose(true)
	} else {
		gitRunner.SetVerbose(opts.Verbose)
		gitRunner.SetQuiet(opts.Quiet)
	}

	// 设置Git LFS
	if opts.GitLFS {
		log.Info("安装 Git LFS...")
		if _, err := gitRunner.Run("lfs", "install"); err != nil {
			log.Error("安装 Git LFS 失败: %v", err)
			return fmt.Errorf("failed to install Git LFS: %w", err)
		}
		log.Info("Git LFS 安装成功")
	}

	// 创建.repo目录结构
	log.Info("创建 repo 目录结构...")
	currentDir, err := os.Getwd()
	if err != nil {
		log.Error("获取当前目录失败: %v", err)
		return fmt.Errorf("failed to get current directory: %w", err)
	}

	// 初始化Git配置和hooks
	log.Debug("初始化repo 目录结构和Git hooks")
	if err := initRepoStructure(currentDir); err != nil {
		log.Error("初始化repo 结构失败: %v", err)
		return fmt.Errorf("failed to initialize repo structure: %w", err)
	}
	log.Info("repo 目录结构创建成功")

	// 克隆清单仓库
	log.Info("克隆清单仓库...")
	if err := cloneManifestRepo(gitRunner, cfg); err != nil {
		log.Error("克隆清单仓库失败: %v", err)
		return fmt.Errorf("failed to clone manifest repository: %w", err)
	}
	log.Info("清单仓库克隆成功")

	// 解析清单文件
	log.Info("解析清单文件...")
	parser := manifest.NewParser()
	parser.SetSilentMode(!opts.Verbose && !opts.Debug) // 根据verbose和debug选项控制警告日志输出
	manifestPath := filepath.Join(".repo", "manifests", cfg.ManifestName)
	log.Debug("解析清单文件: %s", manifestPath)
	manifestObj, err := parser.ParseFromFile(manifestPath, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}
	log.Info("清单文件解析成功，包含 %d 个项目", len(manifestObj.Projects))

	groups := strings.Split(cfg.Groups, ",")
	// 按group过滤项目
	if cfg.Groups != "" {
		log.Info("根据组过滤项目 %v", groups)

		filteredProjects := make([]manifest.Project, 0)
		for _, p := range manifestObj.Projects {
			if p.Groups == "" || containsAnyGroup(p.Groups, groups) {
				filteredProjects = append(filteredProjects, p)
				log.Debug("包含项目: %s (组 %s)", p.Name, p.Groups)
			} else {
				log.Debug("排除项目: %s (组 %s)", p.Name, p.Groups)
			}
		}

		log.Info("过滤后的项目数量: %d (原始数量: %d)", len(filteredProjects), len(manifestObj.Projects))

		manifestObj.Projects = filteredProjects

		// 更新清单对象后重新保
		mergedPath := filepath.Join(".repo", "manifest.xml")
		log.Debug("保存过滤后的清单到 %s", mergedPath)
		mergedData, err := manifestObj.ToXML()
		if err != nil {
			log.Error("转换过滤后的清单为XML失败: %v", err)
			return fmt.Errorf("转换过滤后的清单为XML失败: %w", err)
		}
		if err := os.WriteFile(mergedPath, []byte(mergedData), 0644); err != nil {
			log.Error("写入过滤后的清单文件失败: %v", err)
			return fmt.Errorf("写入过滤后的清单文件失败: %w", err)
		}

		log.Info("已将过滤后的清单保存到 %s", mergedPath)
	}

	// 处理include标签
	if len(manifestObj.Includes) > 0 && !opts.ThisManifestOnly {
		log.Info("处理 %d 个包含的清单文件", len(manifestObj.Includes))

		// 创建清单合并
		merger := manifest.NewMerger(parser, filepath.Join(".repo", "manifests"))

		// 加载所有包含的清单
		includedManifests := []*manifest.Manifest{manifestObj}

		for _, include := range manifestObj.Includes {
			includePath := filepath.Join(".repo", "manifests", include.Name)
			log.Debug("加载包含的清单 %s", include.Name)

			// 检查包含的清单文件是否存在
			if _, err := os.Stat(includePath); os.IsNotExist(err) {
				log.Error("包含的清单文件不存在: %s", includePath)
				return fmt.Errorf("包含的清单文件不存在: %s", includePath)
			}

			includeManifest, err := parser.ParseFromFile(includePath, groups)
			if err != nil {
				log.Error("解析包含的清单文件%s 失败: %v", include.Name, err)
				return fmt.Errorf("解析包含的清单文件%s 失败: %w", include.Name, err)
			}

			if includeManifest == nil {
				log.Error("包含的清单文件 %s 解析结果为空", include.Name)
				return fmt.Errorf("包含的清单文件 %s 解析结果为空", include.Name)
			}

			log.Debug("包含的清单 %s 包含 %d 个项目", include.Name, len(includeManifest.Projects))

			includedManifests = append(includedManifests, includeManifest)
		}

		// 合并清单
		log.Info("合并 %d 个清单文件", len(includedManifests))

		mergedManifest, err := merger.Merge(includedManifests)
		if err != nil {
			log.Error("合并清单失败: %v", err)
			return fmt.Errorf("合并清单失败: %w", err)
		}

		// 更新清单对象
		manifestObj = mergedManifest

		log.Info("合并后的清单包含 %d 个项目", len(manifestObj.Projects))

		// 保存合并后的清单
		mergedPath := filepath.Join(".repo", "manifest.xml")
		log.Debug("保存合并后的清单到 %s", mergedPath)
		mergedData, err := manifestObj.ToXML()
		if err != nil {
			log.Error("转换合并后的清单为XML失败: %v", err)
			return fmt.Errorf("转换合并后的清单为XML失败: %w", err)
		}

		if err := os.WriteFile(mergedPath, []byte(mergedData), 0644); err != nil {
			log.Error("写入合并后的清单文件失败: %v", err)
			return fmt.Errorf("写入合并后的清单文件失败: %w", err)
		}

		log.Info("已将合并后的清单保存到 %s", mergedPath)
	}

	// 保存配置
	log.Info("保存 repo 配置...")
	if err := saveRepoConfig(cfg); err != nil {
		log.Error("保存配置失败: %v", err)
		return fmt.Errorf("failed to save config: %w", err)
	}

	// 处理多清单选项
	if opts.OuterManifest {
		// 实现加载外部清单的逻辑
		log.Info("加载外部清单...")

		// 查找外部清单
		outerManifestPath := filepath.Join("..", ".repo", "manifest.xml")
		if _, err := os.Stat(outerManifestPath); err == nil {
			// 加载外部清单
			log.Debug("解析外部清单: %s", outerManifestPath)
			outerManifest, err := parser.ParseFromFile(outerManifestPath, groups)
			if err != nil {
				log.Error("解析外部清单失败: %v", err)
				return fmt.Errorf("failed to parse outer manifest: %w", err)
			}

			// 合并外部清单
			log.Debug("合并外部清单...")
			merger := manifest.NewMerger(parser, filepath.Join(".repo"))
			mergedManifest, err := merger.Merge([]*manifest.Manifest{outerManifest, manifestObj})
			if err != nil {
				log.Error("合并外部清单失败: %v", err)
				return fmt.Errorf("failed to merge with outer manifest: %w", err)
			}

			// 更新清单对象
			manifestObj = mergedManifest

			// 保存合并后的清单
			mergedPath := filepath.Join(".repo", "manifest.xml")
			log.Debug("保存合并后的清单到 %s", mergedPath)
			mergedData, err := manifestObj.ToXML()
			if err != nil {
				log.Error("转换合并后的清单为XML失败: %v", err)
				return fmt.Errorf("failed to convert merged manifest to XML: %w", err)
			}

			if err := os.WriteFile(mergedPath, []byte(mergedData), 0644); err != nil {
				log.Error("写入合并后的清单文件失败: %v", err)
				return fmt.Errorf("failed to write merged manifest: %w", err)
			}
			log.Info("外部清单合并成功")
		} else {
			log.Debug("未找到外部清单 %s", outerManifestPath)
		}
	}

	if opts.ThisManifestOnly {
		// 实现仅处理当前清单的逻辑
		log.Info("仅处理当前清单")
		// 移除所有include标签
		manifestObj.Includes = nil
	}

	if opts.AllManifests {
		// 实现处理所有清单的逻辑
		log.Info("处理所有清单")
		// 确保处理所有include标签
		if len(manifestObj.Includes) > 0 && !opts.ThisManifestOnly {
			merger := manifest.NewMerger(parser, filepath.Join(".repo", "manifests"))
			includedManifests := []*manifest.Manifest{manifestObj}

			for _, include := range manifestObj.Includes {
				includePath := filepath.Join(".repo", "manifests", include.Name)
				log.Debug("加载包含的清单 %s", include.Name)

				includeManifest, err := parser.ParseFromFile(includePath, groups)
				if err != nil {
					log.Error("解析包含的清单文件%s 失败: %v", include.Name, err)
					return fmt.Errorf("failed to parse included manifest %s: %w", include.Name, err)
				}

				includedManifests = append(includedManifests, includeManifest)
			}

			// 合并清单
			log.Debug("合并所有清单..")
			mergedManifest, err := merger.Merge(includedManifests)
			if err != nil {
				log.Error("合并清单失败: %v", err)
				return fmt.Errorf("failed to merge manifests: %w", err)
			}

			// 更新清单对象
			manifestObj = mergedManifest
			log.Info("所有清单合并成功")
		}
	}

	log.Info("Repo 初始化完成")
	return nil
}

// initRepoStructure 初始化repo目录结构和配置
func initRepoStructure(repoDir string) error {
	// 创建.repo目录结构
	dirs := []string{
		".repo",
		".repo/manifests",
		".repo/project-objects",
		".repo/repo",
		".repo/projects",
		".repo/hooks",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(repoDir, dir), 0755); err != nil {
			return fmt.Errorf("failed to create directory %s: %w", dir, err)
		}
	}

	// 初始化Git hooks
	if err := hook.InitHooks(repoDir); err != nil {
		return fmt.Errorf("failed to initialize hooks: %w", err)
	}

	// 创建repo.git配置文件
	if err := hook.CreateRepoGitConfig(repoDir); err != nil {
		return fmt.Errorf("failed to create repo.git config: %w", err)
	}

	// 创建repo.gitconfig配置文件
	if err := hook.CreateRepoGitconfig(repoDir); err != nil {
		return fmt.Errorf("failed to create repo.gitconfig: %w", err)
	}

	// 记录钩子目录路径，用于后续同步到各个项目
	// hooksDir := filepath.Join(repoDir, ".repo", "hooks")
	// fmt.Printf("已初始化钩子脚本目录: %s\n", hooksDir)

	return nil
}

// containsAnyGroup 检查项目组是否包含任一指定组
func containsAnyGroup(projectGroups string, checkGroups []string) bool {
	// 如果没有指定过滤组，则包含所有项目
	if len(checkGroups) == 0 {
		return true
	}

	// 如果项目没有指定组，则默认包含
	if projectGroups == "" {
		return true
	}

	// 如果传入的是"all"，则包含所有项目
	for _, cg := range checkGroups {
		if cg == "all" {
			return true
		}
	}

	projectGroupList := strings.Split(projectGroups, ",")
	for _, pg := range projectGroupList {
		pg = strings.TrimSpace(pg) // 去除可能的空格
		if pg == "" {
			continue // 跳过空组
		}

		for _, cg := range checkGroups {
			cg = strings.TrimSpace(cg) // 去除可能的空格
			if cg == "" {
				continue // 跳过空组
			}

			if pg == cg {
				return true
			}
		}
	}
	return false
}
