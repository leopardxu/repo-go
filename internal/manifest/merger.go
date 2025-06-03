package manifest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/leopardxu/repo-go/internal/logger"
)

// Merger 负责合并多个清单
type Merger struct {
	Parser  *Parser
	BaseDir string // 清单文件的基础目录
}

// NewMerger 创建清单合并器
func NewMerger(parser *Parser, baseDir string) *Merger {
	return &Merger{
		Parser:  parser,
		BaseDir: baseDir,
	}
}

// Merge 合并多个清单
func (m *Merger) Merge(manifests []*Manifest) (*Manifest, error) {
	if len(manifests) == 0 {
		return nil, fmt.Errorf("没有清单可合并")
	}

	if len(manifests) == 1 {
		logger.Debug("只有一个清单，无需合并")
		return manifests[0], nil
	}

	logger.Info("开始合并%d 个清单", len(manifests))

	// 使用第一个清单作为基础
	result := manifests[0]

	// 合并其他清单
	for i := 1; i < len(manifests); i++ {
		logger.Debug("合并第%d 个清单", i+1)
		if err := m.mergeManifest(result, manifests[i]); err != nil {
			logger.Error("合并第%d 个清单失败: %v", i+1, err)
			return nil, err
		}
	}

	logger.Info("清单合并完成，共 %d 个项目", len(result.Projects))
	return result, nil
}

// mergeManifest 将src清单合并到dst清单
func (m *Merger) mergeManifest(dst, src *Manifest) error {
	if dst == nil || src == nil {
		return fmt.Errorf("源清单或目标清单为空")
	}

	// 合并远程
	remoteCount := 0
	for _, remote := range src.Remotes {
		// 检查是否已存在同名远程
		exists := false
		for _, r := range dst.Remotes {
			if r.Name == remote.Name {
				exists = true
				break
			}
		}

		// 如果不存在，添加到目标清单
		if !exists {
			dst.Remotes = append(dst.Remotes, remote)
			remoteCount++
		}
	}

	if remoteCount > 0 {
		logger.Debug("合并了%d 个远程配置", remoteCount)
	}

	// 合并项目
	addedProjects := 0
	updatedProjects := 0
	skippedProjects := 0

	for _, project := range src.Projects {
		// 检查是否需要移除该项目
		skip := false
		for _, rp := range dst.RemoveProjects {
			if rp.Name == project.Name {
				skip = true
				logger.Debug("跳过已标记为移除的项 %s", project.Name)
				break
			}
		}

		if skip {
			skippedProjects++
			continue
		}

		// 检查是否已存在同名项目
		exists := false
		for i, p := range dst.Projects {
			if p.Name == project.Name {
				// 更新现有项目
				dst.Projects[i] = project
				exists = true
				updatedProjects++
				break
			}
		}

		// 如果不存在，添加到目标清
		if !exists {
			dst.Projects = append(dst.Projects, project)
			addedProjects++
		}
	}

	if addedProjects > 0 || updatedProjects > 0 || skippedProjects > 0 {
		logger.Debug("项目合并结果: 新增 %d 个, 更新 %d 个, 跳过 %d 个",
			addedProjects, updatedProjects, skippedProjects)
	}

	// 合并移除项目
	removedCount := 0
	addedRemoveProjects := 0

	for _, removeProject := range src.RemoveProjects {
		// 检查是否已存在同名移除项目
		exists := false
		for _, rp := range dst.RemoveProjects {
			if rp.Name == removeProject.Name {
				exists = true
				break
			}
		}

		// 如果不存在，添加到目标清
		if !exists {
			dst.RemoveProjects = append(dst.RemoveProjects, removeProject)
			addedRemoveProjects++
			logger.Debug("添加移除项目标记: %s", removeProject.Name)
		}

		// 从项目列表中移除该项
		for i, p := range dst.Projects {
			if p.Name == removeProject.Name {
				// 移除项目
				dst.Projects = append(dst.Projects[:i], dst.Projects[i+1:]...)
				removedCount++
				logger.Debug("从项目列表中移除项目: %s", removeProject.Name)
				break
			}
		}
	}

	if addedRemoveProjects > 0 || removedCount > 0 {
		logger.Debug("处理移除项目: 添加 %d 个移除标记, 实际移除 %d 个项目",
			addedRemoveProjects, removedCount)
	}

	return nil
}

// ProcessIncludes 处理清单中的include标签
func (m *Merger) ProcessIncludes(manifest *Manifest, groups []string) (*Manifest, error) {
	if manifest == nil {
		return nil, fmt.Errorf("清单不能为空")
	}

	if len(manifest.Includes) == 0 {
		logger.Debug("清单没有包含其他清单文件，无需处理")
		return manifest, nil
	}

	logger.Info("处理清单包含%d 个子清单", len(manifest.Includes))

	// 收集所有需要合并的清单
	manifests := []*Manifest{manifest}

	// 处理包含的清单文
	for i, include := range manifest.Includes {
		includePath := filepath.Join(m.BaseDir, include.Name)
		logger.Info("处理包含的清单文(%d/%d): %s", i+1, len(manifest.Includes), include.Name)

		// 检查文件是否存
		if _, err := os.Stat(includePath); os.IsNotExist(err) {
			logger.Error("包含的清单文件不存在: %s", includePath)
			return nil, fmt.Errorf("包含的清单文件不存在: %s", includePath)
		}

		// 显示处理的组信息
		if len(groups) > 0 {
			logger.Debug("使用组过 %s", strings.Join(groups, ", "))
		}

		// 解析包含的清单文
		includeManifest, err := m.Parser.ParseFromFile(includePath, groups)
		if err != nil {
			logger.Error("解析包含的清单文件失 %s, 错误: %v", includePath, err)
			return nil, fmt.Errorf("解析包含的清单文件失 %w", err)
		}

		// 递归处理包含的清单中的include标签
		logger.Debug("递归处理清单 %s 中的包含标签", include.Name)
		processedInclude, err := m.ProcessIncludes(includeManifest, groups)
		if err != nil {
			logger.Error("处理包含的清单中的包含标签失败: %v", err)
			return nil, err
		}

		manifests = append(manifests, processedInclude)
	}

	// 合并所有清单
	logger.Info("合并所有处理后的清单，共%d 个", len(manifests))
	return m.Merge(manifests)
}
