package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cix-code/gogo/internal/config"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
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
	AddManifestFlags(cmd, &opts.CommonManifestOptions)

	return cmd
}

// runManifest 执行manifest命令
func runManifest(opts *ManifestOptions, args []string) error {
	fmt.Println("Processing manifest")

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

	// 如果需要创建快照
	if opts.Snapshot {
		// 创建快照清单
		snapshotManifest, err := createSnapshotManifest(manifest, cfg, opts)
		if err != nil {
			return fmt.Errorf("failed to create snapshot manifest: %w", err)
		}
		
		// 替换原始清单
		manifest = snapshotManifest
	}

	// 如果指定了输出文件
	if opts.OutputFile != "" {
		// 确保输出目录存在
		outputDir := filepath.Dir(opts.OutputFile)
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			return fmt.Errorf("failed to create output directory: %w", err)
		}
		
		// 写入输出文件
		if err := manifest.WriteToFile(opts.OutputFile); err != nil {
			return fmt.Errorf("failed to write manifest to file: %w", err)
		}
		
		fmt.Printf("Manifest written to %s\n", opts.OutputFile)
	} else {
		// 否则，输出到标准输出
		if opts.JsonOutput {
			jsonData, err := manifest.ToJSON()
			if err != nil {
				return fmt.Errorf("failed to convert manifest to JSON: %w", err)
			}
			fmt.Println(jsonData)
		} else {
			xml, err := manifest.ToXML()
			if err != nil {
				return fmt.Errorf("failed to convert manifest to XML: %w", err)
			}
			fmt.Println(xml)
		}
	}

	return nil
}

// createSnapshotManifest 创建快照清单
func createSnapshotManifest(m *manifest.Manifest, cfg *config.Config, opts *ManifestOptions) (*manifest.Manifest, error) {
	// 创建快照清单的副本
	snapshotManifest := *m
	
	fmt.Println("Creating manifest snapshot")
	
	// 创建项目管理器
	projectManager := project.NewManager(&snapshotManifest, cfg)
	
	// 遍历所有项目，获取当前HEAD提交哈希并更新修订版本
	for i, p := range snapshotManifest.Projects {
		// 获取项目对象
		proj := projectManager.GetProject(p.Name)
		if proj == nil {
			fmt.Printf("Warning: project %s not found in workspace, skipping\n", p.Name)
			continue
		}
		
		// 获取当前HEAD提交哈希
		output, err := proj.GitRepo.Runner.RunInDir(proj.Path, "rev-parse", "HEAD")
		if err != nil {
			fmt.Printf("Warning: failed to get HEAD revision for project %s: %v\n", p.Name, err)
			continue
		}
		
		// 获取提交哈希（去除末尾的换行符）
		commitHash := strings.TrimSpace(string(output))
		
		// 根据选项更新修订版本
		if opts.RevisionAsHEAD {
			// 使用HEAD作为修订版本
			snapshotManifest.Projects[i].Revision = "HEAD"
		} else {
			// 使用实际的提交哈希
			snapshotManifest.Projects[i].Revision = commitHash
		}
		
		// 处理SuppressUpstreamRevision选项
		if opts.SuppressUpstreamRevision {
			// 移除上游修订版本信息
			upstreamRevision, exists := snapshotManifest.Projects[i].GetCustomAttr("upstream-revision")
			if exists {
				delete(snapshotManifest.Projects[i].CustomAttrs, "upstream-revision")
				fmt.Printf("Removed upstream-revision %s from project %s\n", upstreamRevision, p.Name)
			}
		}
		
		// 处理SuppressDestBranch选项
		if opts.SuppressDestBranch {
			// 移除目标分支信息
			destBranch, exists := snapshotManifest.Projects[i].GetCustomAttr("dest-branch")
			if exists {
				delete(snapshotManifest.Projects[i].CustomAttrs, "dest-branch")
				fmt.Printf("Removed dest-branch %s from project %s\n", destBranch, p.Name)
			}
		}
		
		// 处理NoCloneBundle选项
		if opts.NoCloneBundle {
			// 添加no-clone-bundle属性
			snapshotManifest.Projects[i].CustomAttrs["no-clone-bundle"] = "true"
		}
		
		fmt.Printf("Updated project %s revision to %s\n", p.Name, snapshotManifest.Projects[i].Revision)
	}
	
	// 处理Platform选项
	if opts.Platform {
		// 在平台模式下，可能需要添加一些特定的属性或修改
		snapshotManifest.CustomAttrs["platform"] = "true"
		fmt.Println("Applied platform mode to manifest")
	}
	
	return &snapshotManifest, nil
}