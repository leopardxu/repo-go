package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/leopardxu/repo-go/internal/config"
	"github.com/leopardxu/repo-go/internal/logger"
	"github.com/leopardxu/repo-go/internal/manifest"
	"github.com/leopardxu/repo-go/internal/project"
	"github.com/spf13/cobra"
)

type ManifestOptions struct {
	CommonManifestOptions
	RevisionAsHEAD          bool
	OutputFile              string
	SuppressUpstreamRevision bool
	SuppressDestBranch      bool
	Snapshot                bool
	NoCloneBundle           bool
	JsonOutput              bool
	PrettyOutput            bool
	NoLocalManifests        bool
	Verbose                 bool
	Quiet                   bool
	Jobs                    int
}

// manifestStats 用于统计manifest命令的执行结�?
type manifestStats struct {
	mu      sync.Mutex
	success int
	failed  int
}

// ManifestCmd 返回manifest命令
func ManifestCmd() *cobra.Command {
	opts := &ManifestOptions{}

	cmd := &cobra.Command{
		Use:   "manifest",
		Short: "Manifest inspection utility",
		Long:  `Manifest inspection utility to view or generate manifest files.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runManifest(opts, args)
		},
	}

	// 添加命令行选项
	cmd.Flags().BoolVarP(&opts.RevisionAsHEAD, "revision-as-HEAD", "r", false, "save revisions as current HEAD")
	cmd.Flags().StringVarP(&opts.OutputFile, "output-file", "o", "", "file to save the manifest to. (Filename prefix for multi-tree.)")
	cmd.Flags().BoolVar(&opts.SuppressUpstreamRevision, "suppress-upstream-revision", false, "if in -r mode, do not write the upstream field (only of use if the branch names for a sha1 manifest are sensitive)")
	cmd.Flags().BoolVar(&opts.SuppressDestBranch, "suppress-dest-branch", false, "if in -r mode, do not write the dest-branch field (only of use if the branch names for a sha1 manifest are sensitive)")
	cmd.Flags().BoolVar(&opts.Snapshot, "snapshot", false, "create a manifest snapshot")
	cmd.Flags().BoolVar(&opts.Platform, "platform", false, "platform manifest")
	cmd.Flags().BoolVar(&opts.NoCloneBundle, "no-clone-bundle", false, "disable use of /clone.bundle on HTTP/HTTPS")
	cmd.Flags().BoolVar(&opts.JsonOutput, "json", false, "output manifest in JSON format (experimental)")
	cmd.Flags().BoolVar(&opts.PrettyOutput, "pretty", false, "format output for humans to read")
	cmd.Flags().BoolVar(&opts.NoLocalManifests, "no-local-manifests", false, "ignore local manifests")
	cmd.Flags().BoolVarP(&opts.Verbose, "verbose", "v", false, "show all output")
	cmd.Flags().BoolVarP(&opts.Quiet, "quiet", "q", false, "only show errors")
	cmd.Flags().IntVarP(&opts.Jobs, "jobs", "j", 8, "number of jobs to run in parallel")
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runManifest 执行manifest命令
func runManifest(opts *ManifestOptions, args []string) error {
	// 初始化日志记录器
	log := logger.NewDefaultLogger()
	if opts.Verbose {
		log.SetLevel(logger.LogLevelDebug)
	} else if opts.Quiet {
		log.SetLevel(logger.LogLevelError)
	} else {
		log.SetLevel(logger.LogLevelInfo)
	}

	log.Info("开始处理清单文�?)

	// 加载配置
	log.Debug("正在加载配置...")
	cfg, err := config.Load()
	if err != nil {
		log.Error("加载配置失败: %v", err)
		return fmt.Errorf("failed to load config: %w", err)
	}

	// 加载清单
	log.Debug("正在解析清单文件...")
	parser := manifest.NewParser()
	manifestObj, err := parser.ParseFromFile(cfg.ManifestName, strings.Split(cfg.Groups, ","))
	if err != nil {
		log.Error("解析清单文件失败: %v", err)
		return fmt.Errorf("failed to parse manifest: %w", err)
	}

	log.Debug("清单文件解析成功，包�?%d 个项�?, len(manifestObj.Projects))

	// 如果需要创建快�?
	if opts.Snapshot {
		log.Info("正在创建清单快照...")
		// 创建快照清单
		snapshotManifest, err := createSnapshotManifest(manifestObj, cfg, opts, log)
		if err != nil {
			log.Error("创建快照清单失败: %v", err)
			return fmt.Errorf("failed to create snapshot manifest: %w", err)
		}
		
		// 替换原始清单
		manifestObj = snapshotManifest
		log.Info("清单快照创建成功")
	}

	// 如果指定了输出文�?
	if opts.OutputFile != "" {
		// 确保输出目录存在
		outputDir := filepath.Dir(opts.OutputFile)
		log.Debug("确保输出目录存在: %s", outputDir)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			log.Error("创建输出目录失败: %v", err)
			return fmt.Errorf("failed to create output directory: %w", err)
		}
		
		// 写入输出文件
		log.Debug("正在写入清单到文�? %s", opts.OutputFile)
		if err := manifestObj.WriteToFile(opts.OutputFile); err != nil {
			log.Error("写入清单到文件失�? %v", err)
			return fmt.Errorf("failed to write manifest to file: %w", err)
		}
		
		log.Info("清单已写入到文件: %s", opts.OutputFile)
	} else {
		// 否则，输出到标准输出
		log.Debug("正在准备输出清单到标准输�?)
		if opts.JsonOutput {
			log.Debug("使用JSON格式输出")
			jsonData, err := manifestObj.ToJSON()
			if err != nil {
				log.Error("转换清单到JSON失败: %v", err)
				return fmt.Errorf("failed to convert manifest to JSON: %w", err)
			}
			fmt.Println(jsonData)
		} else {
			log.Debug("使用XML格式输出")
			xml, err := manifestObj.ToXML()
			if err != nil {
				log.Error("转换清单到XML失败: %v", err)
				return fmt.Errorf("failed to convert manifest to XML: %w", err)
			}
			fmt.Println(xml)
		}
		log.Info("清单输出完成")
	}

	return nil
}

// createSnapshotManifest 创建快照清单
func createSnapshotManifest(m *manifest.Manifest, cfg *config.Config, opts *ManifestOptions, log logger.Logger) (*manifest.Manifest, error) {
	// 创建快照清单的副�?
	snapshotManifest := *m
	
	log.Info("开始创建清单快�?)
	
	// 创建项目管理�?
	log.Debug("正在创建项目管理�?..")
	projectManager := project.NewManagerFromManifest(&snapshotManifest, cfg)
	
	// 并发处理项目更新
	type projectUpdate struct {
		index int
		proj  *project.Project
		err   error
	}

	// 设置并发控制
	maxWorkers := opts.Jobs
	if maxWorkers <= 0 {
		maxWorkers = 8
	}
	log.Debug("设置并发数为: %d", maxWorkers)

	// 创建统计对象
	stats := &manifestStats{}

	// 使用WaitGroup确保所有goroutine完成
	var wg sync.WaitGroup
	sem := make(chan struct{}, maxWorkers)
	results := make(chan projectUpdate, len(snapshotManifest.Projects))

	log.Info("开始处�?%d 个项�?..", len(snapshotManifest.Projects))

	for i, p := range snapshotManifest.Projects {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx int, projName string) {
			defer func() { 
				<-sem 
				wg.Done()
			}()
			update := projectUpdate{index: idx}
			
			// 获取项目对象
			log.Debug("正在获取项目: %s", projName)
			update.proj = projectManager.GetProject(projName)
			if update.proj == nil {
				log.Warn("项目 %s 在工作区中未找到，跳�?, projName)
				
				// 更新统计信息
				stats.mu.Lock()
				stats.failed++
				stats.mu.Unlock()
				
				results <- update
				return
			}
			
			// 获取当前HEAD提交哈希
			log.Debug("正在获取项目 %s 的HEAD提交哈希", projName)
			output, err := update.proj.GitRepo.Runner.RunInDir(update.proj.Path, "rev-parse", "HEAD")
			if err != nil {
				log.Warn("获取项目 %s 的HEAD提交哈希失败: %v", projName, err)
				update.err = err
				
				// 更新统计信息
				stats.mu.Lock()
				stats.failed++
				stats.mu.Unlock()
				
				results <- update
				return
			}
			
			// 获取提交哈希（去除末尾的换行符）
			commitHash := strings.TrimSpace(string(output))
			log.Debug("项目 %s 的HEAD提交哈希: %s", projName, commitHash)
			
			// 根据选项更新修订版本
			if opts.RevisionAsHEAD {
				log.Debug("将项�?%s 的修订版本设置为HEAD", projName)
				snapshotManifest.Projects[update.index].Revision = "HEAD"
			} else {
				log.Debug("将项�?%s 的修订版本设置为提交哈希: %s", projName, commitHash)
				snapshotManifest.Projects[update.index].Revision = commitHash
			}
			
			// 处理SuppressUpstreamRevision选项
			if opts.SuppressUpstreamRevision {
				// 移除上游修订版本信息
				upstreamRevision, exists := snapshotManifest.Projects[update.index].GetCustomAttr("upstream-revision")
				if exists {
					delete(snapshotManifest.Projects[update.index].CustomAttrs, "upstream-revision")
					log.Debug("从项�?%s 中移除上游修订版�? %s", projName, upstreamRevision)
				}
			}
			
			// 处理SuppressDestBranch选项
			if opts.SuppressDestBranch {
				// 移除目标分支信息
				destBranch, exists := snapshotManifest.Projects[update.index].GetCustomAttr("dest-branch")
				if exists {
					delete(snapshotManifest.Projects[update.index].CustomAttrs, "dest-branch")
					log.Debug("从项�?%s 中移除目标分�? %s", projName, destBranch)
				}
			}
			
			// 处理NoCloneBundle选项
			if opts.NoCloneBundle {
				// 添加no-clone-bundle属�?
				snapshotManifest.Projects[update.index].CustomAttrs["no-clone-bundle"] = "true"
				log.Debug("为项�?%s 添加no-clone-bundle属�?, projName)
			}
			
			log.Info("已更新项�?%s 的修订版本为 %s", projName, snapshotManifest.Projects[update.index].Revision)
			
			// 更新统计信息
			stats.mu.Lock()
			stats.success++
			stats.mu.Unlock()
			
			results <- update
		}(i, p.Name)
	}

	// 等待所有goroutine完成
	log.Debug("等待所有项目处理完�?..")
	wg.Wait()
	close(results)

	// 处理Platform选项
	if opts.Platform {
		// 在平台模式下，可能需要添加一些特定的属性或修改
		snapshotManifest.CustomAttrs["platform"] = "true"
		log.Info("已应用平台模式到清单")
	}
	
	// 输出统计信息
	log.Info("清单快照创建完成: %d 个项目成�? %d 个项目失�?, stats.success, stats.failed)
	
	return &snapshotManifest, nil
}
