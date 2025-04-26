package manifest

import (
	"fmt"
	"os"
	"path/filepath"
)

// Merger 负责合并多个清单
type Merger struct {
	Parser *Parser
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
		return nil, fmt.Errorf("no manifests to merge")
	}

	// 使用第一个清单作为基础
	result := manifests[0]

	// 合并其他清单
	for i := 1; i < len(manifests); i++ {
		if err := m.mergeManifest(result, manifests[i]); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// mergeManifest 将src清单合并到dst清单
func (m *Merger) mergeManifest(dst, src *Manifest) error {
	// 合并远程
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
		}
	}

	// 合并项目
	for _, project := range src.Projects {
		// 检查是否需要移除该项目
		skip := false
		for _, rp := range dst.RemoveProjects {
			if rp.Name == project.Name {
				skip = true
				break
			}
		}

		if skip {
			continue
		}

		// 检查是否已存在同名项目
		exists := false
		for i, p := range dst.Projects {
			if p.Name == project.Name {
				// 更新现有项目
				dst.Projects[i] = project
				exists = true
				break
			}
		}

		// 如果不存在，添加到目标清单
		if !exists {
			dst.Projects = append(dst.Projects, project)
		}
	}

	// 合并移除项目
	for _, removeProject := range src.RemoveProjects {
		// 检查是否已存在同名移除项目
		exists := false
		for _, rp := range dst.RemoveProjects {
			if rp.Name == removeProject.Name {
				exists = true
				break
			}
		}

		// 如果不存在，添加到目标清单
		if !exists {
			dst.RemoveProjects = append(dst.RemoveProjects, removeProject)
		}

		// 从项目列表中移除该项目
		for i, p := range dst.Projects {
			if p.Name == removeProject.Name {
				// 移除项目
				dst.Projects = append(dst.Projects[:i], dst.Projects[i+1:]...)
				break
			}
		}
	}

	return nil
}

// ProcessIncludes 处理清单中的include标签
func (m *Merger) ProcessIncludes(manifest *Manifest,groups []string) (*Manifest, error) {
	if len(manifest.Includes) == 0 {
		return manifest, nil
	}

	// 收集所有需要合并的清单
	manifests := []*Manifest{manifest}

	// 处理包含的清单文件
	for _, include := range manifest.Includes {
		includePath := filepath.Join(m.BaseDir, include.Name)
		
		// 检查文件是否存在
		if _, err := os.Stat(includePath); os.IsNotExist(err) {
			return nil, fmt.Errorf("included manifest file not found: %s", includePath)
		}

		// 解析包含的清单文件
		includeManifest, err := m.Parser.ParseFromFile(includePath,groups)
		if err != nil {
			return nil, fmt.Errorf("failed to parse included manifest: %w", err)
		}

		// 递归处理包含的清单中的include标签
		processedInclude, err := m.ProcessIncludes(includeManifest,groups)
		if err != nil {
			return nil, err
		}

		manifests = append(manifests, processedInclude)
	}

	// 合并所有清单
	return m.Merge(manifests)
}