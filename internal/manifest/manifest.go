package manifest

import (
	"encoding/xml"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time" // 添加这一行
)

// Manifest 表示repo的清单文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
// 在现有的 manifest.go 文件中添加以下字段和方法

// Manifest 表示清单文件
type Manifest struct {
	XMLName        xml.Name         `xml:"manifest"`
	Remotes        []Remote        `xml:"remote"`
	Default        Default         `xml:"default"`
	Projects       []Project       `xml:"project"`
	Includes       []Include       `xml:"include"`
	RemoveProjects []RemoveProject `xml:"remove-project"`
	CustomAttrs    map[string]string `xml:"-"` // 存储自定义属性
	
	// 添加与 engine.go 兼容的字段
	Subdir        string // 清单子目录
	RepoDir       string // 仓库目录
	Topdir        string // 顶层目录
	WorkDir       string // 工作目录
	ManifestServer string // 清单服务器
	Server        string // 服务器
	ManifestProject *Project // 清单项目
	RepoProject *Project // 仓库项目
	IsArchive bool // 是否为归档
	CloneFilter string // 克隆过滤器
	PartialCloneExclude string // 部分克隆排除
	
	// 静默模式控制
	SilentMode bool // 是否启用静默模式，不输出非关键日志
}

// GetCustomAttr 获取自定义属性值
func (m *Manifest) GetCustomAttr(name string) (string, bool) {
	val, ok := m.CustomAttrs[name]
	return val, ok
}

// Remote 表示远程Git服务器
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Remote struct {
	Name     string `xml:"name,attr"`
	Fetch    string `xml:"fetch,attr"`
	Review   string `xml:"review,attr,omitempty"`
	Revision string `xml:"revision,attr,omitempty"`
	Alias    string `xml:"alias,attr,omitempty"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (r *Remote) GetCustomAttr(name string) (string, bool) {
	val, ok := r.CustomAttrs[name]
	return val, ok
}

// Default 表示默认设置
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Default struct {
	Remote   string `xml:"remote,attr"`
	Revision string `xml:"revision,attr"`
	Sync     string `xml:"sync,attr,omitempty"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (d *Default) GetCustomAttr(name string) (string, bool) {
	val, ok := d.CustomAttrs[name]
	return val, ok
}

// Project 表示一个Git项目
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Project struct {
	Name       string     `xml:"name,attr"`
	Path       string     `xml:"path,attr,omitempty"`
	Remote     string     `xml:"remote,attr,omitempty"`
	Revision   string     `xml:"revision,attr,omitempty"`
	Groups     string     `xml:"groups,attr,omitempty"`
	SyncC      bool       `xml:"sync-c,attr,omitempty"`
	SyncS      bool       `xml:"sync-s,attr,omitempty"`
	CloneDepth int        `xml:"clone-depth,attr,omitempty"`
	Copyfiles  []Copyfile `xml:"copyfile"`
	Linkfiles  []Linkfile `xml:"linkfile"`
	References string     `xml:"references,attr,omitempty"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
	
	// 添加与 engine.go 兼容的字段
	LastFetch  time.Time  // 最后一次获取的时间
	NeedGC     bool       // 是否需要垃圾回收
}

// GetCustomAttr 获取自定义属性值
func (p *Project) GetCustomAttr(name string) (string, bool) {
	val, ok := p.CustomAttrs[name]
	return val, ok
}

// GetBranch 获取当前分支
func (p *Project) GetBranch() (string, error) {
	if p == nil {
		return "", fmt.Errorf("project is nil")
	}
	return p.Revision, nil
}

// Include 表示包含的清单文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Include struct {
	Name string `xml:"name,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
	manifest *Manifest
}

// GetOuterManifest returns the outermost manifest in the include chain
func (i *Include) GetOuterManifest() *Manifest {
	if i.manifest == nil {
		return nil
	}
	return i.manifest.GetOuterManifest()
}

// GetInnerManifest returns the innermost manifest in the include chain
func (i *Include) GetInnerManifest() *Manifest {
	if i.manifest == nil {
		return nil
	}
	return i.manifest.GetInnerManifest()
}

// GetCustomAttr 获取自定义属性值
func (i *Include) GetCustomAttr(name string) (string, bool) {
	val, ok := i.CustomAttrs[name]
	return val, ok
}





// RemoveProject 表示要移除的项目
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type RemoveProject struct {
	Name string `xml:"name,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (r *RemoveProject) GetCustomAttr(name string) (string, bool) {
	val, ok := r.CustomAttrs[name]
	return val, ok
}

// Copyfile 表示要复制的文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Copyfile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (c *Copyfile) GetCustomAttr(name string) (string, bool) {
	val, ok := c.CustomAttrs[name]
	return val, ok
}

// Linkfile 表示要链接的文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
type Linkfile struct {
	Src  string `xml:"src,attr"`
	Dest string `xml:"dest,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (l *Linkfile) GetCustomAttr(name string) (string, bool) {
	val, ok := l.CustomAttrs[name]
	return val, ok
}

// ToJSON 将清单转换为JSON格式
func (m *Manifest) ToJSON() (string, error) {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("failed to marshal manifest to JSON: %w", err)
	}
	return string(data), nil
}

// GetRemoteURL 根据远程名称获取对应的URL
func (m *Manifest) GetRemoteURL(remoteName string) (string, error) {
	for _, remote := range m.Remotes {
		if remote.Name == remoteName {
			return remote.Fetch, nil
		}
	}
	return "", fmt.Errorf("remote %s not found", remoteName)
}

// GetOuterManifest 获取最外层的清单
func (m *Manifest) GetOuterManifest() *Manifest {
	if m.Includes == nil || len(m.Includes) == 0 {
		return m
	}
	return m.Includes[0].GetOuterManifest()
}

// GetInnerManifest 获取最内层的清单
func (m *Manifest) GetInnerManifest() *Manifest {
	if m.Includes == nil || len(m.Includes) == 0 {
		return m
	}
	return m.Includes[len(m.Includes)-1].GetInnerManifest()
}

// GetThisManifest 获取当前清单
func (m *Manifest) GetThisManifest() *Manifest {
	return m
}

// Parser 负责解析清单文件
type Parser struct {
	silentMode bool
	// 配置项
}

// NewParser 创建清单解析器
// 这是一个包级别函数，供外部调用
func NewParser() *Parser {
	return &Parser{}
}


// ParseFromFile 从文件解析清单
func (p *Parser) ParseFromFile(filename string, groups []string) (*Manifest, error) {
	// 处理清单文件路径
	// 构建可能的路径列表
	paths := []string{}
	
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get current working directory: %w", err)
	}
	
	// 查找顶层仓库目录
	topDir := findTopLevelRepoDir(cwd)
	if topDir == "" {
		topDir = cwd // 如果找不到顶层目录，使用当前目录
	}
	
	// 1. 首先尝试直接使用manifest.xml（优先级最高）
	paths = append(paths, ".repo/manifest.xml")
	paths = append(paths, filepath.Join(cwd, ".repo", "manifest.xml"))
	paths = append(paths, filepath.Join(topDir, ".repo", "manifest.xml"))
	
	// 2. 原始路径
	paths = append(paths, filename)
	
	// 3. 如果是相对路径
	if !filepath.IsAbs(filename) {
		// 2.1 添加.repo前缀（如果还没有）
		if !strings.HasPrefix(filename, ".repo") {
			paths = append(paths, filepath.Join(".repo", filename))
			// 添加基于当前工作目录和顶层目录的绝对路径
			paths = append(paths, filepath.Join(cwd, ".repo", filename))
			paths = append(paths, filepath.Join(topDir, ".repo", filename))
		}
		
		// 2.2 尝试.repo/manifests/目录
		paths = append(paths, filepath.Join(".repo", "manifests", filename))
		paths = append(paths, filepath.Join(cwd, ".repo", "manifests", filename))
		paths = append(paths, filepath.Join(topDir, ".repo", "manifests", filename))
		
		// 2.3 只使用文件名，在.repo/manifests/目录下查找
		paths = append(paths, filepath.Join(".repo", "manifests", filepath.Base(filename)))
		paths = append(paths, filepath.Join(cwd, ".repo", "manifests", filepath.Base(filename)))
		paths = append(paths, filepath.Join(topDir, ".repo", "manifests", filepath.Base(filename)))
		
		// 2.4 尝试当前目录
		paths = append(paths, filepath.Join(".", filename))
		paths = append(paths, filepath.Join(cwd, filename))
		paths = append(paths, filepath.Join(topDir, filename))
	}
	
	// 3. 如果是绝对路径，也尝试其他可能的位置
	if filepath.IsAbs(filename) {
		base := filepath.Base(filename)
		paths = append(paths, filepath.Join(".repo", base))
		paths = append(paths, filepath.Join(cwd, ".repo", base))
		paths = append(paths, filepath.Join(topDir, ".repo", base))
		paths = append(paths, filepath.Join(".repo", "manifests", base))
		paths = append(paths, filepath.Join(cwd, ".repo", "manifests", base))
		paths = append(paths, filepath.Join(topDir, ".repo", "manifests", base))
	}
	
	// 去除重复的路径
	uniquePaths := make([]string, 0, len(paths))
	pathMap := make(map[string]bool)
	for _, path := range paths {
		// 规范化路径
		normalizedPath := filepath.Clean(path)
		if !pathMap[normalizedPath] {
			pathMap[normalizedPath] = true
			uniquePaths = append(uniquePaths, normalizedPath)
		}
	}
	paths = uniquePaths
	
	// 只在非静默模式下打印调试信息
	if !p.silentMode {
		fmt.Printf("正在查找清单文件，尝试以下路径:\n")
		for _, path := range paths {
			fmt.Printf("  - %s\n", path)
		}
	}

	// 尝试读取文件
	var data []byte
	var readErr error
	var foundFile bool
	var successPath string

	for _, path := range paths {
		data, readErr = ioutil.ReadFile(path)
		if readErr == nil {
			foundFile = true
			successPath = path
			break
		}
	}

	if !foundFile {
		// 检查.repo目录是否存在
		repoPath := filepath.Join(cwd, ".repo")
		if _, dirErr := os.Stat(repoPath); os.IsNotExist(dirErr) {
			return nil, fmt.Errorf(".repo目录不存在，请先运行 'repo init' 命令: %w", dirErr)
		}
		
		// 检查.repo/manifest.xml是否存在
		manifestPath := filepath.Join(repoPath, "manifest.xml")
		if _, manifestErr := os.Stat(manifestPath); os.IsNotExist(manifestErr) {
			return nil, fmt.Errorf(".repo目录中未找到manifest.xml文件，请先运行 'repo init' 命令: %w", manifestErr)
		}
		
		return nil, fmt.Errorf("无法从任何可能的位置读取清单文件 (已尝试 %v): %w", paths, readErr)
	}

	// 只在非静默模式下打印调试信息
	if !p.silentMode {
		fmt.Printf("成功从以下位置加载清单: %s\n", successPath)
		
		// 打印文件内容的前100个字符，便于调试
		if len(data) > 0 {
			previewLen := 100
			if len(data) < previewLen {
				previewLen = len(data)
			}
			fmt.Printf("清单内容预览: %s...\n", data[:previewLen])
		} else {
			fmt.Printf("警告: 清单文件为空!\n")
		}
	}
	
	// 确保groups参数正确处理
	var groupsToUse []string
	if groups != nil {
		groupsToUse = groups
	}
	
	return p.Parse(data, groupsToUse)
}

// ParseFromBytes 从字节数据解析清单
func (p *Parser) ParseFromBytes(data []byte, groups []string) (*Manifest, error) {
    if len(data) == 0 {
        return nil, fmt.Errorf("manifest data is empty")
    }
    return p.Parse(data, groups)
}

// Parse 解析清单数据
func (p *Parser) Parse(data []byte, groups []string) (*Manifest, error) {
	// 首先使用标准解析
	var manifest Manifest
	if err := xml.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析清单XML失败: %w", err)
	}

	// 初始化所有结构体的CustomAttrs字段
	manifest.CustomAttrs = make(map[string]string)
	manifest.Default.CustomAttrs = make(map[string]string)

	// 初始化新添加的字段
	manifest.IsArchive = false // 默认不是归档
	manifest.CloneFilter = "" // 默认无克隆过滤器
	manifest.PartialCloneExclude = "" // 默认无部分克隆排除
	
	// 尝试从自定义属性中获取值
	if isArchive, ok := manifest.GetCustomAttr("is-archive"); ok {
		manifest.IsArchive = isArchive == "true"
	}
	if cloneFilter, ok := manifest.GetCustomAttr("clone-filter"); ok {
		manifest.CloneFilter = cloneFilter
	}
	if partialCloneExclude, ok := manifest.GetCustomAttr("partial-clone-exclude"); ok {
		manifest.PartialCloneExclude = partialCloneExclude
	}
	
	for i := range manifest.Remotes {
		manifest.Remotes[i].CustomAttrs = make(map[string]string)
	}

	for i := range manifest.Projects {
		manifest.Projects[i].CustomAttrs = make(map[string]string)
		// 如果项目没有指定路径，则使用项目名称作为默认路径
		if manifest.Projects[i].Path == "" {
			manifest.Projects[i].Path = manifest.Projects[i].Name
			// fmt.Printf("Project %s does not specify path, using name as default path\n", manifest.Projects[i].Name)
		}
		// 如果项目没有指定远程仓库，则使用默认远程仓库
		if manifest.Projects[i].Remote == "" {
			manifest.Projects[i].Remote = manifest.Default.Remote
			// fmt.Printf("Project %s does not specify remote, using default remote %s\n", manifest.Projects[i].Name, manifest.Default.Remote)
		}
		// 如果项目没有指定修订版本，则使用默认修订版本
		if manifest.Projects[i].Revision == "" {
			manifest.Projects[i].Revision = manifest.Default.Revision
			// fmt.Printf("Project %s does not specify revision, using default revision %s\n", manifest.Projects[i].Name, manifest.Default.Revision)
		}
		// 验证远程仓库是否存在
		remoteExists := false
		var remoteObj *Remote
		for j := range manifest.Remotes {
			if manifest.Remotes[j].Name == manifest.Projects[i].Remote {
				remoteExists = true
				remoteObj = &manifest.Remotes[j]
				break
			}
		}
		if !remoteExists {
			// 如果找不到远程仓库，记录警告但不中断处理
			// fmt.Printf("Warning: project %s references non-existent remote %s, this may cause sync failures\n", manifest.Projects[i].Name, manifest.Projects[i].Remote)
		} else {
			// 记录远程仓库的Fetch属性，用于后续构建完整URL
			manifest.Projects[i].CustomAttrs["__remote_fetch"] = remoteObj.Fetch
			
			// 构建完整的远程URL并存储在自定义属性中
			remoteURL := remoteObj.Fetch
			if !strings.HasSuffix(remoteURL, "/") {
				remoteURL += "/"
			}
			remoteURL += manifest.Projects[i].Name
			manifest.Projects[i].CustomAttrs["__remote_url"] = remoteURL
		}
		for j := range manifest.Projects[i].Copyfiles {
			manifest.Projects[i].Copyfiles[j].CustomAttrs = make(map[string]string)
		}
		for j := range manifest.Projects[i].Linkfiles {
			manifest.Projects[i].Linkfiles[j].CustomAttrs = make(map[string]string)
		}
	}

	for i := range manifest.Includes {
		manifest.Includes[i].CustomAttrs = make(map[string]string)
	}

	for i := range manifest.RemoveProjects {
		manifest.RemoveProjects[i].CustomAttrs = make(map[string]string)
	}

	// 解析自定义属性
	if err := parseCustomAttributes(data, &manifest); err != nil {
		return nil, fmt.Errorf("failed to parse custom attributes: %w", err)
	}

	// 处理包含的清单文件
	if err := p.processIncludes(&manifest,groups); err != nil {
		return nil, fmt.Errorf("failed to process included manifests: %w", err)
	}

	// 根据groups过滤项目
	if len(groups) > 0 && !containsAll(groups) {
		if !p.silentMode {
			fmt.Printf("根据以下组过滤项目: %v\n", groups)
		}
		
		filteredProjects := make([]Project, 0)
		for _, proj := range manifest.Projects {
			if shouldIncludeProject(proj, groups) {
				filteredProjects = append(filteredProjects, proj)
				if !p.silentMode {
					fmt.Printf("包含项目: %s (组: %s)\n", proj.Name, proj.Groups)
				}
			} else if !p.silentMode {
				fmt.Printf("排除项目: %s (组: %s)\n", proj.Name, proj.Groups)
			}
		}
		
		if !p.silentMode {
			fmt.Printf("过滤后的项目数量: %d (原始数量: %d)\n", len(filteredProjects), len(manifest.Projects))
		}
		
		manifest.Projects = filteredProjects
	}

	return &manifest, nil
}

// processIncludes 处理包含的清单文件
func (p *Parser) processIncludes(manifest *Manifest, groups []string) error {
	if len(manifest.Includes) == 0 {
		return nil
	}
	
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}
	
	// 查找顶层仓库目录
	topDir := findTopLevelRepoDir(cwd)
	if topDir == "" {
		topDir = cwd // 如果找不到顶层目录，使用当前目录
	}
	
	// 处理每个包含的清单文件
	for i := range manifest.Includes {
		includeName := manifest.Includes[i].Name
		
		// 构建可能的路径
		paths := []string{}
		
		// 尝试在.repo/manifests/目录下查找
		paths = append(paths, filepath.Join(".repo", "manifests", includeName))
		paths = append(paths, filepath.Join(cwd, ".repo", "manifests", includeName))
		paths = append(paths, filepath.Join(topDir, ".repo", "manifests", includeName))
		
		// 尝试直接使用路径
		paths = append(paths, includeName)
		paths = append(paths, filepath.Join(cwd, includeName))
		paths = append(paths, filepath.Join(topDir, includeName))
		
		// 去除重复的路径
		uniquePaths := make([]string, 0, len(paths))
		pathMap := make(map[string]bool)
		for _, path := range paths {
			normalizedPath := filepath.Clean(path)
			if !pathMap[normalizedPath] {
				pathMap[normalizedPath] = true
				uniquePaths = append(uniquePaths, normalizedPath)
			}
		}
		paths = uniquePaths
		
		// 尝试读取文件
		var data []byte
		var readErr error
		var foundFile bool
		
		for _, path := range paths {
			data, readErr = ioutil.ReadFile(path)
			if readErr == nil {
				foundFile = true
				break
			}
		}
		
		if !foundFile {
			return fmt.Errorf("failed to read included manifest file %s: %w", includeName, readErr)
		}
		
		// 解析包含的清单文件
		includedManifest, err := p.Parse(data,groups)
		if err != nil {
			return fmt.Errorf("failed to parse included manifest %s: %w", includeName, err)
		}
		
		// 设置包含关系
		manifest.Includes[i].manifest = includedManifest
		
		// 合并远程仓库列表
		for _, remote := range includedManifest.Remotes {
			// 检查是否已存在相同名称的远程仓库
			var exists bool
			for _, existingRemote := range manifest.Remotes {
				if existingRemote.Name == remote.Name {
					exists = true
					break
				}
			}
			if !exists {
				manifest.Remotes = append(manifest.Remotes, remote)
			}
		}
		
		// 合并项目列表
		manifest.Projects = append(manifest.Projects, includedManifest.Projects...)
		
		// 合并移除项目列表
		manifest.RemoveProjects = append(manifest.RemoveProjects, includedManifest.RemoveProjects...)
	}
	
	return nil
}

// CreateRepoStructure 创建.repo目录结构
func (m *Manifest) CreateRepoStructure() error {
	// 创建.repo目录
	if err := os.MkdirAll(".repo", 0755); err != nil {
		return fmt.Errorf("failed to create .repo directory: %w", err)
	}

	// 创建.repo/manifests目录
	if err := os.MkdirAll(".repo/manifests", 0755); err != nil {
		return fmt.Errorf("failed to create .repo/manifests directory: %w", err)
	}

	// 创建.repo/project-objects目录
	if err := os.MkdirAll(".repo/project-objects", 0755); err != nil {
		return fmt.Errorf("failed to create .repo/project-objects directory: %w", err)
	}

	// 创建.repo/projects目录
	if err := os.MkdirAll(".repo/projects", 0755); err != nil {
		return fmt.Errorf("failed to create .repo/projects directory: %w", err)
	}

	// 创建.repo/hooks目录
	if err := os.MkdirAll(".repo/hooks", 0755); err != nil {
		return fmt.Errorf("failed to create .repo/hooks directory: %w", err)
	}

	return nil
}

// GitRunner 和 Config 结构体在这里定义，但实际的克隆逻辑在clone.go中实现

// GitRunner 接口定义
type GitRunner interface {
	Run(args ...string) ([]byte, error)
}

// Config 配置结构体
type Config struct {
	ManifestURL    string
	ManifestBranch string
	ManifestName   string
	Mirror         bool
	Reference      string
	Depth          int
}

// parseCustomAttributes 解析XML中的自定义属性
func parseCustomAttributes(data []byte, manifest *Manifest) error {
	// 创建一个临时结构来存储所有XML元素及其属性
	type rawManifest struct {
		XMLName        xml.Name     `xml:"manifest"`
		Attrs          []xml.Attr   `xml:",any,attr"`
		Remotes        []struct {
			XMLName xml.Name   `xml:"remote"`
			Attrs   []xml.Attr `xml:",any,attr"`
		} `xml:"remote"`
		Default        struct {
			XMLName xml.Name   `xml:"default"`
			Attrs   []xml.Attr `xml:",any,attr"`
		} `xml:"default"`
		Projects       []struct {
			XMLName   xml.Name   `xml:"project"`
			Attrs     []xml.Attr `xml:",any,attr"`
			Copyfiles []struct {
				XMLName xml.Name   `xml:"copyfile"`
				Attrs   []xml.Attr `xml:",any,attr"`
			} `xml:"copyfile"`
			Linkfiles []struct {
				XMLName xml.Name   `xml:"linkfile"`
				Attrs   []xml.Attr `xml:",any,attr"`
			} `xml:"linkfile"`
		} `xml:"project"`
		Includes       []struct {
			XMLName xml.Name   `xml:"include"`
			Attrs   []xml.Attr `xml:",any,attr"`
		} `xml:"include"`
		RemoveProjects []struct {
			XMLName xml.Name   `xml:"remove-project"`
			Attrs   []xml.Attr `xml:",any,attr"`
		} `xml:"remove-project"`
	}

	var raw rawManifest

	// 解析XML以获取所有属性
	if err := xml.Unmarshal(data, &raw); err != nil {
		return err
	}

	// 处理Manifest级别的自定义属性
	for _, attr := range raw.Attrs {
		if !isStandardManifestAttr(attr.Name.Local) {
			manifest.CustomAttrs[attr.Name.Local] = attr.Value
		}
	}

	// 处理Default的自定义属性
	for _, attr := range raw.Default.Attrs {
		if !isStandardDefaultAttr(attr.Name.Local) {
			manifest.Default.CustomAttrs[attr.Name.Local] = attr.Value
		}
	}

	// 处理Remote的自定义属性
	for i, remote := range raw.Remotes {
		if i < len(manifest.Remotes) {
			for _, attr := range remote.Attrs {
				if !isStandardRemoteAttr(attr.Name.Local) {
					manifest.Remotes[i].CustomAttrs[attr.Name.Local] = attr.Value
				}
			}
		}
	}

	// 处理Project的自定义属性
	for i, project := range raw.Projects {
		if i < len(manifest.Projects) {
			for _, attr := range project.Attrs {
				if !isStandardProjectAttr(attr.Name.Local) {
					manifest.Projects[i].CustomAttrs[attr.Name.Local] = attr.Value
				}
			}

			// 处理Copyfile的自定义属性
			for j, copyfile := range project.Copyfiles {
				if j < len(manifest.Projects[i].Copyfiles) {
					for _, attr := range copyfile.Attrs {
						if !isStandardCopyfileAttr(attr.Name.Local) {
							manifest.Projects[i].Copyfiles[j].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
				}
			}

			// 处理Linkfile的自定义属性
			for j, linkfile := range project.Linkfiles {
				if j < len(manifest.Projects[i].Linkfiles) {
					for _, attr := range linkfile.Attrs {
						if !isStandardLinkfileAttr(attr.Name.Local) {
							manifest.Projects[i].Linkfiles[j].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
				}
			}
		}
	}

	// 处理Include的自定义属性
	for i, include := range raw.Includes {
		if i < len(manifest.Includes) {
			for _, attr := range include.Attrs {
				if !isStandardIncludeAttr(attr.Name.Local) {
					manifest.Includes[i].CustomAttrs[attr.Name.Local] = attr.Value
				}
			}
		}
	}

	// 处理RemoveProject的自定义属性
	for i, removeProject := range raw.RemoveProjects {
		if i < len(manifest.RemoveProjects) {
			for _, attr := range removeProject.Attrs {
				if !isStandardRemoveProjectAttr(attr.Name.Local) {
					manifest.RemoveProjects[i].CustomAttrs[attr.Name.Local] = attr.Value
				}
			}
		}
	}

	return nil
}

// findTopLevelRepoDir 查找包含.repo目录的顶层目录
func findTopLevelRepoDir(startDir string) string {
	// 从当前目录开始向上查找，直到找到包含.repo目录的目录
	dir := startDir
	for {
		// 检查当前目录是否包含.repo目录
		repoDir := filepath.Join(dir, ".repo")
		if _, err := os.Stat(repoDir); err == nil {
			// 找到了.repo目录
			return dir
		}
		
		// 获取父目录
		parent := filepath.Dir(dir)
		if parent == dir {
			// 已经到达根目录，没有找到.repo目录
			return ""
		}
		dir = parent
	}
}

// 以下是用于检查属性是否为标准属性的辅助函数
func isStandardManifestAttr(name string) bool {
	// Manifest没有标准属性
	return false
}

func isStandardDefaultAttr(name string) bool {
	switch name {
	case "remote", "revision", "sync":
		return true
	}
	return false
}

func isStandardRemoteAttr(name string) bool {
	switch name {
	case "name", "fetch", "review", "revision", "alias":
		return true
	}
	return false
}

func isStandardProjectAttr(name string) bool {
	switch name {
	case "name", "path", "remote", "revision", "groups", "sync-c", "sync-s", "clone-depth":
		return true
	}
	return false
}

func isStandardCopyfileAttr(name string) bool {
	switch name {
	case "src", "dest":
		return true
	}
	return false
}

func isStandardLinkfileAttr(name string) bool {
	switch name {
	case "src", "dest":
		return true
	}
	return false
}

func isStandardIncludeAttr(name string) bool {
	switch name {
	case "name":
		return true
	}
	return false
}

func isStandardRemoveProjectAttr(name string) bool {
	switch name {
	case "name":
		return true
	}
	return false
}

// WriteToFile 将清单写入文件
func (m *Manifest) WriteToFile(filename string) error {
	xml, err := m.ToXML()
	if err != nil {
		return err
	}
	
	return os.WriteFile(filename, []byte(xml), 0644)
}

// ToXML 将清单转换为XML字符串
func (m *Manifest) ToXML() (string, error) {
	// 实现XML序列化逻辑
	// 这里是一个简单的实现，实际应用中可能需要更复杂的逻辑
	
	// 创建XML头
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<manifest>
`
	
	// 添加默认设置
	xml += fmt.Sprintf(`  <default remote="%s" revision="%s"`, m.Default.Remote, m.Default.Revision)
	// 添加默认设置的自定义属性
	for k, v := range m.Default.CustomAttrs {
		xml += fmt.Sprintf(` %s="%s"`, k, v)
	}
	xml += " />\n"
	
	// 添加远程仓库
	for _, r := range m.Remotes {
		xml += fmt.Sprintf(`  <remote name="%s" fetch="%s"`, r.Name, r.Fetch)
		if r.Review != "" {
			xml += fmt.Sprintf(` review="%s"`, r.Review)
		}
		if r.Revision != "" {
			xml += fmt.Sprintf(` revision="%s"`, r.Revision)
		}
		if r.Alias != "" {
			xml += fmt.Sprintf(` alias="%s"`, r.Alias)
		}
		// 添加远程仓库的自定义属性
		for k, v := range r.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		xml += " />\n"
	}
	
	// 添加包含的清单文件
	for _, i := range m.Includes {
		xml += fmt.Sprintf(`  <include name="%s"`, i.Name)
		// 添加包含清单的自定义属性
		for k, v := range i.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		xml += " />\n"
	}
	
	// 添加项目
	for _, p := range m.Projects {
		xml += fmt.Sprintf(`  <project name="%s"`, p.Name)
		if p.Path != "" {
			xml += fmt.Sprintf(` path="%s"`, p.Path)
		}
		if p.Remote != "" {
			xml += fmt.Sprintf(` remote="%s"`, p.Remote)
		}
		if p.Revision != "" {
			xml += fmt.Sprintf(` revision="%s"`, p.Revision)
		}
		if p.Groups != "" {
			xml += fmt.Sprintf(` groups="%s"`, p.Groups)
		}
		if p.SyncC {
			xml += ` sync-c="true"`
		}
		if p.SyncS {
			xml += ` sync-s="true"`
		}
		if p.CloneDepth > 0 {
			xml += fmt.Sprintf(` clone-depth="%d"`, p.CloneDepth)
		}
		
		// 添加项目的自定义属性
		for k, v := range p.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		
		// 检查是否有copyfile或linkfile子元素
		if len(p.Copyfiles) > 0 || len(p.Linkfiles) > 0 {
			xml += ">\n"
			
			// 添加copyfile子元素
			for _, c := range p.Copyfiles {
				xml += fmt.Sprintf(`    <copyfile src="%s" dest="%s"`, c.Src, c.Dest)
				// 添加copyfile的自定义属性
				for k, v := range c.CustomAttrs {
					xml += fmt.Sprintf(` %s="%s"`, k, v)
				}
				xml += " />\n"
			}
			
			// 添加linkfile子元素
			for _, l := range p.Linkfiles {
				xml += fmt.Sprintf(`    <linkfile src="%s" dest="%s"`, l.Src, l.Dest)
				// 添加linkfile的自定义属性
				for k, v := range l.CustomAttrs {
					xml += fmt.Sprintf(` %s="%s"`, k, v)
				}
				xml += " />\n"
			}
			
			xml += "  </project>\n"
		} else {
			xml += " />\n"
		}
	}
	
	// 添加移除项目
	for _, r := range m.RemoveProjects {
		xml += fmt.Sprintf(`  <remove-project name="%s"`, r.Name)
		// 添加移除项目的自定义属性
		for k, v := range r.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		xml += " />\n"
	}
	
	// 关闭XML
	xml += "</manifest>\n"
	
	return xml, nil
}

func (m *Manifest) ParseFromBytes(data []byte,groups []string) error {
    if len(data) == 0 {
        return fmt.Errorf("manifest data is empty")
    }

    // 创建临时解析器
    parser := NewParser()
    
    // 使用解析器解析数据
    parsedManifest, err := parser.Parse(data,groups)
    if err != nil {
        return fmt.Errorf("failed to parse manifest data: %w", err)
    }

    // 更新当前manifest对象
    *m = *parsedManifest
    
    // 设置清单文件路径相关字段
    if m.RepoDir == "" {
        m.RepoDir = ".repo"
    }
    if m.Topdir == "" {
        if cwd, err := os.Getwd(); err == nil {
            m.Topdir = cwd
        }
    }
    
    return nil
}

func (m *Manifest) GetCurrentBranch() string {
    if m == nil || m.Default.Revision == "" {
        return ""
    }
    return m.Default.Revision
}


func (p *Parser) SetSilentMode(silent bool) {
    p.silentMode = silent
}

func shouldIncludeProject(proj Project, groups []string) bool {
    // 如果没有指定过滤组，则包含所有项目
    if len(groups) == 0 {
        return true
    }
    
    // 如果项目没有指定groups，则默认包含
    if proj.Groups == "" {
        return true
    }
    
    // 如果传入的是"all"，则包含所有项目
    for _, g := range groups {
        if g == "all" {
            return true
        }
    }
    
    // 检查项目groups是否包含任一传入的group
    projGroups := strings.Split(proj.Groups, ",")
    for _, pg := range projGroups {
        pg = strings.TrimSpace(pg) // 去除可能的空格
        if pg == "" {
            continue // 跳过空组
        }
        
        for _, g := range groups {
            g = strings.TrimSpace(g) // 去除可能的空格
            if g == "" {
                continue // 跳过空组
            }
            
            if pg == g {
                return true
            }
        }
    }
    
    return false
}

// containsAll checks if groups contains "all"
func containsAll(groups []string) bool {
    for _, g := range groups {
        if g == "all" {
            return true
        }
    }
    return false
}
