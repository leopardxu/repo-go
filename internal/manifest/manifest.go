package manifest

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// 定义错误类型
type ManifestError struct {
	Op   string // 操作名称
	Path string // 文件路径
	Err  error  // 原始错误
}

func (e *ManifestError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("manifest %s: %v", e.Op, e.Err)
	}
	return fmt.Sprintf("manifest %s %s: %v", e.Op, e.Path, e.Err)
}

func (e *ManifestError) Unwrap() error {
	return e.Err
}

// 全局缓存
var (
	manifestCache    = make(map[string]*Manifest)
	manifestCacheMux sync.RWMutex
	fileModTimeCache = make(map[string]time.Time)
	fileModTimeMux   sync.RWMutex
)

// Manifest 表示repo的清单文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属性
// 在现有的 manifest.go 文件中添加以下字段和方法

// Manifest 表示清单文件
type Manifest struct {
	XMLName        xml.Name          `xml:"manifest"`
	Remotes        []Remote          `xml:"remote"`
	Default        Default           `xml:"default"`
	Projects       []Project         `xml:"project"`
	ExtendProjects []ExtendProject   `xml:"extend-project"` // 扩展已有项目
	Includes       []Include         `xml:"include"`
	RemoveProjects []RemoveProject   `xml:"remove-project"`
	RepoHooks      *RepoHooks        `xml:"repo-hooks"`                     // repo钩子配置
	Superproject   *Superproject     `xml:"superproject"`                   // 超级项目配置
	ManifestServer string            `xml:"manifest-server,attr,omitempty"` // manifest服务器
	CustomAttrs    map[string]string `xml:"-"`                              // 存储自定义属性

	// 添加与engine.go 兼容的字段
	Subdir              string   // 清单子目录
	RepoDir             string   // 仓库目录
	Topdir              string   // 顶层目录
	WorkDir             string   // 工作目录
	Server              string   // 服务器
	ManifestProject     *Project // 清单项目
	RepoProject         *Project // 仓库项目
	IsArchive           bool     // 是否为归档
	CloneFilter         string   // 克隆过滤器
	PartialCloneExclude string   // 部分克隆排除

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
	Name        string            `xml:"name,attr"`
	Fetch       string            `xml:"fetch,attr"`
	Review      string            `xml:"review,attr,omitempty"`
	Revision    string            `xml:"revision,attr,omitempty"`
	Alias       string            `xml:"alias,attr,omitempty"`
	PushURL     string            `xml:"pushurl,attr,omitempty"` // 新增：推送URL
	CustomAttrs map[string]string `xml:"-"`                      // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (r *Remote) GetCustomAttr(name string) (string, bool) {
	val, ok := r.CustomAttrs[name]
	return val, ok
}

// Default 表示默认设置
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属
type Default struct {
	Remote      string            `xml:"remote,attr"`
	Revision    string            `xml:"revision,attr"`
	DestBranch  string            `xml:"dest-branch,attr,omitempty"` // 默认目标分支
	Upstream    string            `xml:"upstream,attr,omitempty"`    // 默认上游分支
	SyncJ       int               `xml:"sync-j,attr,omitempty"`      // 默认并发数
	SyncC       bool              `xml:"sync-c,attr,omitempty"`      // 默认仅同步当前分支
	SyncS       bool              `xml:"sync-s,attr,omitempty"`      // 默认同步子模块
	SyncTags    bool              `xml:"sync-tags,attr,omitempty"`   // 默认同步标签
	Sync        string            `xml:"sync,attr,omitempty"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属
}

// GetCustomAttr 获取自定义属性值
func (d *Default) GetCustomAttr(name string) (string, bool) {
	val, ok := d.CustomAttrs[name]
	return val, ok
}

// Project 表示一个Git项目
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属
type Project struct {
	Name        string            `xml:"name,attr"`
	Path        string            `xml:"path,attr,omitempty"`
	Remote      string            `xml:"remote,attr,omitempty"`
	Revision    string            `xml:"revision,attr,omitempty"`
	Upstream    string            `xml:"upstream,attr,omitempty"`    // 上游分支，用于跟踪
	DestBranch  string            `xml:"dest-branch,attr,omitempty"` // 目标分支，用于上传审查
	Groups      string            `xml:"groups,attr,omitempty"`
	SyncC       bool              `xml:"sync-c,attr,omitempty"`
	SyncS       bool              `xml:"sync-s,attr,omitempty"`
	CloneDepth  int               `xml:"clone-depth,attr,omitempty"`
	Copyfiles   []Copyfile        `xml:"copyfile"`
	Linkfiles   []Linkfile        `xml:"linkfile"`
	Annotations []Annotation      `xml:"annotation"` // 项目注解
	References  string            `xml:"references,attr,omitempty"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属

	// 添加engine.go 兼容的字
	LastFetch time.Time // 最后一次获取的时间
	NeedGC    bool      // 是否需要垃圾回
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

// Include 表示包含的清单文
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属
type Include struct {
	Name        string            `xml:"name,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属
	manifest    *Manifest
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
	Name        string            `xml:"name,attr"`
	Path        string            `xml:"path,attr,omitempty"`     // 新增：路径
	Optional    bool              `xml:"optional,attr,omitempty"` // 新增：可选标志
	CustomAttrs map[string]string `xml:"-"`                       // 存储自定义属性
}

// GetCustomAttr 获取自定义属性值
func (r *RemoveProject) GetCustomAttr(name string) (string, bool) {
	val, ok := r.CustomAttrs[name]
	return val, ok
}

// Copyfile 表示要复制的文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属
type Copyfile struct {
	Src         string            `xml:"src,attr"`
	Dest        string            `xml:"dest,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属
}

// GetCustomAttr 获取自定义属性值
func (c *Copyfile) GetCustomAttr(name string) (string, bool) {
	val, ok := c.CustomAttrs[name]
	return val, ok
}

// Linkfile 表示要链接的文件
// 支持自定义属性，可以通过CustomAttrs字段访问未在结构体中定义的XML属
type Linkfile struct {
	Src         string            `xml:"src,attr"`
	Dest        string            `xml:"dest,attr"`
	CustomAttrs map[string]string `xml:"-"` // 存储自定义属
}

// GetCustomAttr 获取自定义属性值
func (l *Linkfile) GetCustomAttr(name string) (string, bool) {
	val, ok := l.CustomAttrs[name]
	return val, ok
}

// detectIncludeCycle 检测include循环引用
func (p *Parser) detectIncludeCycle(filename string) bool {
	if p.visitedFiles == nil {
		p.visitedFiles = make(map[string]bool)
	}
	if p.visitedFiles[filename] {
		return true // 发现循环引用
	}
	p.visitedFiles[filename] = true
	return false
}

// removeVisitedFile 从访问记录中移除文件（用于递归处理）
func (p *Parser) removeVisitedFile(filename string) {
	if p.visitedFiles != nil {
		delete(p.visitedFiles, filename)
	}
}

// Annotation 表示项目注解
type Annotation struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
	Keep  string `xml:"keep,attr,omitempty"` // 新增：保留标志，默认为"true"
}

// ExtendProject 表示扩展已有项目的配置
type ExtendProject struct {
	Name      string     `xml:"name,attr"`
	Path      string     `xml:"path,attr,omitempty"`
	Groups    string     `xml:"groups,attr,omitempty"`
	Revision  string     `xml:"revision,attr,omitempty"`
	Remote    string     `xml:"remote,attr,omitempty"`
	Copyfiles []Copyfile `xml:"copyfile"`
	Linkfiles []Linkfile `xml:"linkfile"`
}

// RepoHooks 表示repo钩子配置
type RepoHooks struct {
	InProject   string `xml:"in-project,attr"`
	EnabledList string `xml:"enabled-list,attr,omitempty"`
}

// Superproject 表示超级项目配置
type Superproject struct {
	Name     string `xml:"name,attr"`
	Remote   string `xml:"remote,attr,omitempty"`
	Revision string `xml:"revision,attr,omitempty"` // 新增：修订版本
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

// GetOuterManifest 获取最外层的清
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

// replaceVariables 替换内容中的变量引用，支持 ${VAR} 和 $VAR 格式
func (p *Parser) replaceVariables(content []byte, envVars map[string]string) []byte {
	// 首先处理 ${VAR} 格式
	re := regexp.MustCompile(`\$\{([^}]+)\}`)
	result := re.ReplaceAllFunc(content, func(match []byte) []byte {
		varName := string(match[2 : len(match)-1])
		if val, exists := envVars[varName]; exists {
			return []byte(val)
		}
		// 如果变量未定义，保持原样
		return match
	})

	// 然后处理 $VAR 格式（单个字母或数字的变量名）
	reSimple := regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)
	result = reSimple.ReplaceAllFunc(result, func(match []byte) []byte {
		varName := string(match[1:])
		if val, exists := envVars[varName]; exists {
			return []byte(val)
		}
		// 如果变量未定义，保持原样
		return match
	})

	return result
}

// getEnvVars 获取环境变量映射
func (p *Parser) getEnvVars() map[string]string {
	vars := make(map[string]string)
	for _, env := range os.Environ() {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			vars[parts[0]] = parts[1]
		}
	}
	return vars
}

// GetThisManifest 获取当前清单
func (m *Manifest) GetThisManifest() *Manifest {
	return m
}

// 全局静默模式设置
var (
	globalSilentMode bool = false
)

// SetSilentMode 设置全局静默模式
func SetSilentMode(silent bool) {
	globalSilentMode = silent
}

// Parser 负责解析清单文件
type Parser struct {
	silentMode   bool
	cacheEnabled bool
	visitedFiles map[string]bool // 用于检测循环引用
}

// NewParser 创建清单解析器
// 这是一个包级别函数，供外部调用
func NewParser() *Parser {
	return &Parser{
		silentMode:   globalSilentMode,
		cacheEnabled: true,
		visitedFiles: make(map[string]bool),
	}
}

// SetParserSilentMode 设置解析器的静默模式
func (p *Parser) SetSilentMode(silent bool) {
	p.silentMode = silent
}

// SetCacheEnabled 设置是否启用缓存
func (p *Parser) SetCacheEnabled(enabled bool) {
	p.cacheEnabled = enabled
}

// ParseFromFile 从文件解析清单
func (p *Parser) ParseFromFile(filename string, groups []string) (*Manifest, error) {
	// 检查参数
	if filename == "" {
		return nil, &ManifestError{Op: "parse", Err: fmt.Errorf("文件名不能为空")}
	}

	// 查找文件
	successPath, err := p.findManifestFile(filename)
	if err != nil {
		return nil, err
	}

	// 检查缓存
	if p.cacheEnabled {
		manifestCacheMux.RLock()
		fileModTimeMux.RLock()
		cachedManifest, hasCachedManifest := manifestCache[successPath]
		cachedModTime, hasCachedModTime := fileModTimeCache[successPath]
		fileModTimeMux.RUnlock()
		manifestCacheMux.RUnlock()

		if hasCachedManifest && hasCachedModTime {
			// 检查文件是否被修改
			fileInfo, err := os.Stat(successPath)
			if err == nil && !fileInfo.ModTime().After(cachedModTime) {
				// 文件未被修改，使用缓存

				// 创建副本以避免修改缓存
				manifestCopy := *cachedManifest

				// 应用组过
				if len(groups) > 0 && !containsAll(groups) {
					return p.filterProjectsByGroups(&manifestCopy, groups)
				}

				return &manifestCopy, nil
			}
		}
	}

	// 读取文件
	data, err := ioutil.ReadFile(successPath)
	if err != nil {
		return nil, &ManifestError{Op: "read", Path: successPath, Err: err}
	}

	// 记录文件信息
	if len(data) == 0 {
		// 清单文件为空
	}

	// 解析数据
	manifest, err := p.Parse(data, groups)
	if err != nil {
		return nil, err
	}

	// 更新缓存
	if p.cacheEnabled {
		fileInfo, err := os.Stat(successPath)
		if err == nil {
			// 创建副本以避免缓存被修改
			manifestCopy := *manifest

			manifestCacheMux.Lock()
			fileModTimeMux.Lock()
			manifestCache[successPath] = &manifestCopy
			fileModTimeCache[successPath] = fileInfo.ModTime()
			fileModTimeMux.Unlock()
			manifestCacheMux.Unlock()
		}
	}

	return manifest, nil
}

// findManifestFile 查找清单文件的实际路径
func (p *Parser) findManifestFile(filename string) (string, error) {
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return "", &ManifestError{Op: "find", Err: fmt.Errorf("无法获取当前工作目录: %w", err)}
	}

	// 查找顶层仓库目录
	topDir := findTopLevelRepoDir(cwd)
	if topDir == "" {
		topDir = cwd // 如果找不到顶层目录，使用当前目录
	}

	// 构建可能的路径列
	paths := []string{}

	// 1. 首先尝试直接使用manifest.xml（优先级最高）
	paths = append(paths, ".repo/manifest.xml")
	paths = append(paths, filepath.Join(cwd, ".repo", "manifest.xml"))
	paths = append(paths, filepath.Join(topDir, ".repo", "manifest.xml"))

	// 2. 原始路径
	paths = append(paths, filename)

	// 3. 如果是相对路
	if !filepath.IsAbs(filename) {
		// 2.1 添加.repo前缀（如果还没有
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

		// 2.3 只使用文件名，在.repo/manifests/目录下查
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

	// 去除重复的路
	uniquePaths := make([]string, 0, len(paths))
	pathMap := make(map[string]bool)
	for _, path := range paths {
		// 规范化路
		normalizedPath := filepath.Clean(path)
		if !pathMap[normalizedPath] {
			pathMap[normalizedPath] = true
			uniquePaths = append(uniquePaths, normalizedPath)
		}
	}
	paths = uniquePaths

	// 尝试查找清单文件

	// 尝试读取文件
	for _, path := range paths {
		if fileExists(path) {
			return path, nil
		}
	}

	// 检repo目录是否存在
	repoPath := filepath.Join(cwd, ".repo")
	if !fileExists(repoPath) {
		return "", &ManifestError{Op: "find", Err: fmt.Errorf(".repo目录不存在，请先运行 'repo init' 命令")}
	}

	// 检repo/manifest.xml是否存在
	manifestPath := filepath.Join(repoPath, "manifest.xml")
	if !fileExists(manifestPath) {
		return "", &ManifestError{Op: "find", Err: fmt.Errorf(".repo目录中未找到manifest.xml文件，请先运'repo init' 命令")}
	}

	return "", &ManifestError{Op: "find", Err: fmt.Errorf("无法从任何可能的位置找到清单文件 (已尝%d 个路", len(paths))}
}

// fileExists 检查文件是否存
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// filterProjectsByGroups 根据组过滤项
func (p *Parser) filterProjectsByGroups(manifest *Manifest, groups []string) (*Manifest, error) {
	if len(groups) == 0 || containsAll(groups) {
		return manifest, nil
	}

	filteredProjects := make([]Project, 0)
	for _, proj := range manifest.Projects {
		if shouldIncludeProject(proj, groups) {
			filteredProjects = append(filteredProjects, proj)
		}
	}

	// 过滤项目完成

	manifest.Projects = filteredProjects
	return manifest, nil
}

// ParseFromBytes 从字节数据解析清
func (p *Parser) ParseFromBytes(data []byte, groups []string) (*Manifest, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("manifest data is empty")
	}
	return p.Parse(data, groups)
}

// Parse 解析清单数据
func (p *Parser) Parse(data []byte, groups []string) (*Manifest, error) {
	// 首先进行变量替换
	envVars := p.getEnvVars()
	processedData := p.replaceVariables(data, envVars)

	// 使用处理后的数据进行标准解析
	var manifest Manifest
	if err := xml.Unmarshal(processedData, &manifest); err != nil {
		return nil, &ManifestError{Op: "parse", Err: fmt.Errorf("解析清单XML失败: %w", err)}
	}

	// 初始化所有结构体的CustomAttrs字段
	manifest.CustomAttrs = make(map[string]string)
	manifest.Default.CustomAttrs = make(map[string]string)

	// 初始化新添加的字
	manifest.IsArchive = false        // 默认不是归档
	manifest.CloneFilter = ""         // 默认无克隆过滤器
	manifest.PartialCloneExclude = "" // 默认无部分克隆排

	// 尝试从自定义属性中获取
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

	// 当 default remote 为空且只有一个 remote 时，将该 remote 作为默认值
	if manifest.Default.Remote == "" && len(manifest.Remotes) == 1 {
		defaultRemote := manifest.Remotes[0]
		manifest.Default.Remote = defaultRemote.Name

		// 如果该 remote 有定义 revision，则将其作为默认的 revision
		if manifest.Default.Revision == "" && defaultRemote.Revision != "" {
			manifest.Default.Revision = defaultRemote.Revision
		}
	}

	// 处理项目

	for i := range manifest.Projects {
		manifest.Projects[i].CustomAttrs = make(map[string]string)
		// 如果项目没有指定路径，则使用项目名称作为默认路径
		if manifest.Projects[i].Path == "" {
			manifest.Projects[i].Path = manifest.Projects[i].Name
		}
		// 如果项目没有指定远程仓库，则使用默认远程仓库
		if manifest.Projects[i].Remote == "" {
			manifest.Projects[i].Remote = manifest.Default.Remote
		}
		// 如果项目没有指定修订版本，则使用默认修订版本
		if manifest.Projects[i].Revision == "" {
			manifest.Projects[i].Revision = manifest.Default.Revision
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
			// 如果找不到远程仓库，不中断处理
		} else {
			// 记录远程仓库的Fetch属性，用于后续构建完整URL
			manifest.Projects[i].CustomAttrs["__remote_fetch"] = remoteObj.Fetch

			// 构建完整的远程URL并存储在自定义属性中
			remoteURL := remoteObj.Fetch
			if !strings.HasSuffix(remoteURL, "/") {
				remoteURL += "/"
			}
			remoteURL += manifest.Projects[i].Name

			// 存储完整的远程URL
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

	// 解析自定义属
	if err := parseCustomAttributes(data, &manifest); err != nil {
		return nil, &ManifestError{Op: "parse_custom_attrs", Err: err}
	}

	// 处理包含的清单文
	if err := p.processIncludes(&manifest, groups); err != nil {
		return nil, &ManifestError{Op: "process_includes", Err: err}
	}

	// 应用ExtendProject配置
	if err := p.applyExtendProjects(&manifest); err != nil {
		return nil, &ManifestError{Op: "apply_extend_projects", Err: err}
	}

	// 处理local_manifests（如果存在）
	if err := p.processLocalManifests(&manifest, groups); err != nil {
		// local_manifests是可选的，如果出错只记录警告
		if !p.silentMode {
			// local_manifests处理失败，继续执行
		}
	}

	// 对项目列表进行去重处理（使用 name + path 组合作为唯一标识）
	deduplicatedProjects := make([]Project, 0)
	projectKeyMap := make(map[string]bool) // 用于跟踪 name+path 组合

	for _, proj := range manifest.Projects {
		// 使用 name + path 组合作为唯一标识
		path := proj.Path
		if path == "" {
			path = proj.Name // 如果path为空，使用name作为path
		}
		key := proj.Name + "@@" + path // 使用特殊分隔符避免冲突

		// 如果 name+path 组合已存在，则跳过
		if projectKeyMap[key] {
			continue
		}

		// 标记 name+path 组合为已处理
		projectKeyMap[key] = true

		// 添加到去重后的列表
		deduplicatedProjects = append(deduplicatedProjects, proj)
	}

	// 更新项目列表
	manifest.Projects = deduplicatedProjects

	// 根据groups过滤项目
	if len(groups) > 0 && !containsAll(groups) {
		return p.filterProjectsByGroups(&manifest, groups)
	}

	return &manifest, nil
}

// parseCustomAttributes 解析XML中的自定义属
func parseCustomAttributes(data []byte, manifest *Manifest) error {
	// 创建一个临时结构来解析XML
	type xmlNode struct {
		XMLName xml.Name   `xml:""`
		Attrs   []xml.Attr `xml:",any,attr"`
		Nodes   []xmlNode  `xml:",any"`
	}

	// 解析XML到临时结
	var root xmlNode
	if err := xml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("解析XML失败: %w", err)
	}

	// 处理根节点的属
	for _, attr := range root.Attrs {
		// 跳过已知属
		if isStandardManifestAttr(attr.Name.Local) {
			continue
		}
		// 存储自定义属
		manifest.CustomAttrs[attr.Name.Local] = attr.Value
	}

	// 处理子节
	for _, node := range root.Nodes {
		switch node.XMLName.Local {
		case "remote":
			// 查找匹配的远程仓
			var name string
			for _, attr := range node.Attrs {
				if attr.Name.Local == "name" {
					name = attr.Value
					break
				}
			}
			// 找到匹配的远程仓库并添加自定义属
			for i, remote := range manifest.Remotes {
				if remote.Name == name {
					for _, attr := range node.Attrs {
						if !isKnownRemoteAttr(attr.Name.Local) {
							manifest.Remotes[i].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
					break
				}
			}
		case "default":
			// 处理默认设置的自定义属
			for _, attr := range node.Attrs {
				if !isKnownDefaultAttr(attr.Name.Local) {
					manifest.Default.CustomAttrs[attr.Name.Local] = attr.Value
				}
			}
		case "project":
			// 查找匹配的项
			var name string
			for _, attr := range node.Attrs {
				if attr.Name.Local == "name" {
					name = attr.Value
					break
				}
			}
			// 找到匹配的项目并添加自定义属
			for i, project := range manifest.Projects {
				if project.Name == name {
					for _, attr := range node.Attrs {
						if !isKnownProjectAttr(attr.Name.Local) {
							manifest.Projects[i].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
					// 处理项目的子节点（copyfile和linkfile
					for _, subNode := range node.Nodes {
						switch subNode.XMLName.Local {
						case "copyfile":
							// 查找匹配的copyfile
							var src, dest string
							for _, attr := range subNode.Attrs {
								if attr.Name.Local == "src" {
									src = attr.Value
								} else if attr.Name.Local == "dest" {
									dest = attr.Value
								}
							}
							// 找到匹配的copyfile并添加自定义属
							for j, copyfile := range manifest.Projects[i].Copyfiles {
								if copyfile.Src == src && copyfile.Dest == dest {
									for _, attr := range subNode.Attrs {
										if !isKnownCopyfileAttr(attr.Name.Local) {
											manifest.Projects[i].Copyfiles[j].CustomAttrs[attr.Name.Local] = attr.Value
										}
									}
									break
								}
							}
						case "linkfile":
							// 查找匹配的linkfile
							var src, dest string
							for _, attr := range subNode.Attrs {
								if attr.Name.Local == "src" {
									src = attr.Value
								} else if attr.Name.Local == "dest" {
									dest = attr.Value
								}
							}
							// 找到匹配的linkfile并添加自定义属
							for j, linkfile := range manifest.Projects[i].Linkfiles {
								if linkfile.Src == src && linkfile.Dest == dest {
									for _, attr := range subNode.Attrs {
										if !isKnownLinkfileAttr(attr.Name.Local) {
											manifest.Projects[i].Linkfiles[j].CustomAttrs[attr.Name.Local] = attr.Value
										}
									}
									break
								}
							}
						}
					}
					break
				}
			}
		case "include":
			// 查找匹配的include
			var name string
			for _, attr := range node.Attrs {
				if attr.Name.Local == "name" {
					name = attr.Value
					break
				}
			}
			// 找到匹配的include并添加自定义属
			for i, include := range manifest.Includes {
				if include.Name == name {
					for _, attr := range node.Attrs {
						if !isKnownIncludeAttr(attr.Name.Local) {
							manifest.Includes[i].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
					break
				}
			}
		case "remove-project":
			// 查找匹配的remove-project
			var name string
			for _, attr := range node.Attrs {
				if attr.Name.Local == "name" {
					name = attr.Value
					break
				}
			}
			// 找到匹配的remove-project并添加自定义属
			for i, removeProject := range manifest.RemoveProjects {
				if removeProject.Name == name {
					for _, attr := range node.Attrs {
						if !isKnownRemoveProjectAttr(attr.Name.Local) {
							manifest.RemoveProjects[i].CustomAttrs[attr.Name.Local] = attr.Value
						}
					}
					break
				}
			}
		}
	}

	return nil
}

// findTopLevelRepoDir 查找包含.repo目录的顶层目
func findTopLevelRepoDir(startDir string) string {
	currentDir := startDir

	// 最多向上查0层目
	for i := 0; i < 10; i++ {
		// 检查当前目录是否包repo目录
		repoDir := filepath.Join(currentDir, ".repo")
		if fileExists(repoDir) {
			return currentDir
		}

		// 获取父目
		parentDir := filepath.Dir(currentDir)

		// 如果已经到达根目录，则停止查
		if parentDir == currentDir {
			break
		}

		currentDir = parentDir
	}

	return ""
}

// 此函数已在文件前面定义，这里删除重复声明
// filterProjectsByGroups 根据组过滤项
// 已删除重复声

// processIncludes 处理包含的清单文件
func (p *Parser) processIncludes(manifest *Manifest, groups []string) error {
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return &ManifestError{Op: "process_includes", Err: fmt.Errorf("无法获取当前工作目录: %w", err)}
	}

	// 查找顶层仓库目录
	topDir := findTopLevelRepoDir(cwd)
	if topDir == "" {
		topDir = cwd // 如果找不到顶层目录，使用当前目录
	}

	// 处理所有包含的清单文件
	for i, include := range manifest.Includes {
		includeName := include.Name

		// 检测循环引用
		if p.detectIncludeCycle(includeName) {
			return fmt.Errorf("检测到include循环引用: %s", includeName)
		}

		// 构建可能的路径
		paths := []string{}

		// 尝试repo/manifests/目录下查找
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
			// 移除访问记录，因为文件不存在
			p.removeVisitedFile(includeName)
			return fmt.Errorf("failed to read included manifest file %s: %w", includeName, readErr)
		}

		// 解析包含的清单文件
		includedManifest, err := p.Parse(data, groups)
		// 解析完成后移除访问记录，允许在其他路径中再次包含
		p.removeVisitedFile(includeName)
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

// applyExtendProjects 应用ExtendProject配置到现有项目
func (p *Parser) applyExtendProjects(manifest *Manifest) error {
	if len(manifest.ExtendProjects) == 0 {
		return nil
	}

	// 创建项目名称到索引的映射
	projectMap := make(map[string]int)
	for i, proj := range manifest.Projects {
		projectMap[proj.Name] = i
		if proj.Path != "" && proj.Path != proj.Name {
			projectMap[proj.Path] = i
		}
	}

	// 应用每个ExtendProject
	for _, extProj := range manifest.ExtendProjects {
		// 查找对应的项目
		idx, found := projectMap[extProj.Name]
		if !found {
			// 如果通过name找不到，尝试通过path查找
			if extProj.Path != "" {
				idx, found = projectMap[extProj.Path]
			}
		}

		if !found {
			// ExtendProject引用的项目不存在，记录警告并跳过
			if !p.silentMode {
				// 警告：extend-project引用的项目不存在
			}
			continue
		}

		// 应用扩展配置（增量更新，而非覆盖）
		proj := &manifest.Projects[idx]

		// 更新path（如果指定）
		if extProj.Path != "" {
			proj.Path = extProj.Path
		}

		// 更新groups（如果指定）
		if extProj.Groups != "" {
			proj.Groups = extProj.Groups
		}

		// 更新revision（如果指定）
		if extProj.Revision != "" {
			proj.Revision = extProj.Revision
		}

		// 更新remote（如果指定）
		if extProj.Remote != "" {
			proj.Remote = extProj.Remote
		}

		// 追加copyfiles
		if len(extProj.Copyfiles) > 0 {
			proj.Copyfiles = append(proj.Copyfiles, extProj.Copyfiles...)
		}

		// 追加linkfiles
		if len(extProj.Linkfiles) > 0 {
			proj.Linkfiles = append(proj.Linkfiles, extProj.Linkfiles...)
		}
	}

	return nil
}

// processLocalManifests 处理.repo/local_manifests目录下的所有manifest文件
func (p *Parser) processLocalManifests(manifest *Manifest, groups []string) error {
	// 获取当前工作目录
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("无法获取当前工作目录: %w", err)
	}

	// 查找顶层仓库目录
	topDir := findTopLevelRepoDir(cwd)
	if topDir == "" {
		topDir = cwd
	}

	// 构建local_manifests目录路径
	localManifestsDir := filepath.Join(topDir, ".repo", "local_manifests")

	// 检查目录是否存在
	if _, err := os.Stat(localManifestsDir); os.IsNotExist(err) {
		// local_manifests目录不存在，这是正常的
		return nil
	}

	// 读取目录中的所有.xml文件
	files, err := os.ReadDir(localManifestsDir)
	if err != nil {
		return fmt.Errorf("读取local_manifests目录失败: %w", err)
	}

	// 按文件名排序以确保确定性顺序
	var xmlFiles []string
	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".xml") {
			xmlFiles = append(xmlFiles, filepath.Join(localManifestsDir, file.Name()))
		}
	}

	if len(xmlFiles) == 0 {
		return nil
	}

	// 处理每个local manifest文件
	for _, xmlFile := range xmlFiles {
		data, err := ioutil.ReadFile(xmlFile)
		if err != nil {
			// 读取失败，记录警告并继续
			if !p.silentMode {
				// 警告：读取local manifest文件失败
			}
			continue
		}

		// 解析local manifest
		localManifest, err := p.Parse(data, groups)
		if err != nil {
			// 解析失败，记录警告并继续
			if !p.silentMode {
				// 警告：解析local manifest文件失败
			}
			continue
		}

		// 合并到主manifest
		if err := p.mergeLocalManifest(manifest, localManifest); err != nil {
			// 合并失败，记录警告并继续
			if !p.silentMode {
				// 警告：合并local manifest失败
			}
			continue
		}
	}

	return nil
}

// mergeLocalManifest 将local manifest合并到主manifest
func (p *Parser) mergeLocalManifest(main *Manifest, local *Manifest) error {
	// 合并remotes
	for _, remote := range local.Remotes {
		exists := false
		for _, r := range main.Remotes {
			if r.Name == remote.Name {
				exists = true
				break
			}
		}
		if !exists {
			main.Remotes = append(main.Remotes, remote)
		}
	}

	// 合并projects（覆盖同名项目）
	projectMap := make(map[string]int)
	for i, proj := range main.Projects {
		projectMap[proj.Name] = i
	}

	for _, proj := range local.Projects {
		if idx, exists := projectMap[proj.Name]; exists {
			// 覆盖现有项目
			main.Projects[idx] = proj
		} else {
			// 添加新项目
			main.Projects = append(main.Projects, proj)
			projectMap[proj.Name] = len(main.Projects) - 1
		}
	}

	// 处理remove-project
	for _, rp := range local.RemoveProjects {
		// 从主manifest中移除项目
		for i := 0; i < len(main.Projects); i++ {
			if main.Projects[i].Name == rp.Name {
				main.Projects = append(main.Projects[:i], main.Projects[i+1:]...)
				i--
			}
		}
	}

	// 应用extend-project
	for _, extProj := range local.ExtendProjects {
		if idx, exists := projectMap[extProj.Name]; exists {
			proj := &main.Projects[idx]
			// 增量更新
			if extProj.Path != "" {
				proj.Path = extProj.Path
			}
			if extProj.Groups != "" {
				proj.Groups = extProj.Groups
			}
			if extProj.Revision != "" {
				proj.Revision = extProj.Revision
			}
			if extProj.Remote != "" {
				proj.Remote = extProj.Remote
			}
			if len(extProj.Copyfiles) > 0 {
				proj.Copyfiles = append(proj.Copyfiles, extProj.Copyfiles...)
			}
			if len(extProj.Linkfiles) > 0 {
				proj.Linkfiles = append(proj.Linkfiles, extProj.Linkfiles...)
			}
		}
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

// GitRunner Config 结构体在这里定义，但实际的克隆逻辑在clone.go中实

// GitRunner 接口定义
type GitRunner interface {
	Run(args ...string) ([]byte, error)
}

// Config 配置结构
type Config struct {
	ManifestURL    string
	ManifestBranch string
	ManifestName   string
	Mirror         bool
	Reference      string
	Depth          int
}

// 此函数已在文件前面定义，这里删除重复声明
// parseCustomAttributes 解析XML中的自定义属
// 已删除重复声

// 此函数已在文件前面定义，这里删除重复声明
// findTopLevelRepoDir 查找包含.repo目录的顶层目
// 已删除重复声

// 以下是用于检查属性是否为标准属性的辅助函数
func isStandardManifestAttr(name string) bool {
	// Manifest没有标准属
	return false
}

func isStandardDefaultAttr(name string) bool {
	switch name {
	case "remote", "revision", "sync", "dest-branch", "upstream", "sync-j", "sync-c", "sync-s", "sync-tags":
		return true
	}
	return false
}

// 以下是用于检查属性是否为已知属性的辅助函数
func isKnownManifestAttr(name string) bool {
	return isStandardManifestAttr(name)
}

func isKnownDefaultAttr(name string) bool {
	return isStandardDefaultAttr(name)
}

func isKnownRemoteAttr(name string) bool {
	return isStandardRemoteAttr(name)
}

func isStandardRemoteAttr(name string) bool {
	switch name {
	case "name", "fetch", "review", "revision", "alias", "pushurl":
		return true
	}
	return false
}

func isStandardProjectAttr(name string) bool {
	switch name {
	case "name", "path", "remote", "revision", "upstream", "dest-branch", "groups", "sync-c", "sync-s", "clone-depth", "references":
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

func isStandardAnnotationAttr(name string) bool {
	switch name {
	case "name", "value", "keep":
		return true
	}
	return false
}

func isStandardRemoveProjectAttr(name string) bool {
	switch name {
	case "name", "path", "optional":
		return true
	}
	return false
}

// 以下是用于检查属性是否为已知属性的辅助函数
func isKnownProjectAttr(name string) bool {
	return isStandardProjectAttr(name)
}

func isKnownCopyfileAttr(name string) bool {
	return isStandardCopyfileAttr(name)
}

func isKnownLinkfileAttr(name string) bool {
	return isStandardLinkfileAttr(name)
}

func isKnownIncludeAttr(name string) bool {
	return isStandardIncludeAttr(name)
}

func isKnownRemoveProjectAttr(name string) bool {
	return isStandardRemoveProjectAttr(name)
}

// 这些函数已在前面定义，这里删除重复声明

func isStandardIncludeAttr(name string) bool {
	switch name {
	case "name", "groups", "revision":
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

// ToXML 将清单转换为XML字符
func (m *Manifest) ToXML() (string, error) {
	// 实现XML序列化逻辑
	// 这里是一个简单的实现，实际应用中可能需要更复杂的逻辑

	// 创建XML
	xml := `<?xml version="1.0" encoding="UTF-8"?>
<manifest>
`
	// 添加默认设置
	defaultRemote := m.Default.Remote
	defaultRevision := m.Default.Revision

	// 由于所有项目都已经有明确的 remote 和 revision 属性，
	// 不再需要从 default.xml 获取默认值
	// 如果默认值为空，使用空字符串即可，因为项目已经有了自己的属性

	// 添加默认设置
	xml += fmt.Sprintf(`  <default remote="%s" revision="%s"`, defaultRemote, defaultRevision)
	// 添加默认设置的自定义属
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
		if r.PushURL != "" {
			xml += fmt.Sprintf(` pushurl="%s"`, r.PushURL)
		}
		// 添加远程仓库的自定义属性
		for k, v := range r.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		xml += " />\n"
	}

	// 不输出包含的清单文件（init 合并后不需要 include 标签）
	// for _, i := range m.Includes {
	// 	xml += fmt.Sprintf(`  <include name="%s"`, i.Name)
	// 	// 添加包含清单的自定义属
	// 	for k, v := range i.CustomAttrs {
	// 		xml += fmt.Sprintf(` %s="%s"`, k, v)
	// 	}
	// 	xml += " />\n"
	// }

	// 添加项目
	for _, p := range m.Projects {
		xml += fmt.Sprintf(`  <project name="%s"`, p.Name)

		// 始终包含 path 属性，如果为空则使用项目名称
		if p.Path != "" {
			xml += fmt.Sprintf(` path="%s"`, p.Path)
		} else {
			xml += fmt.Sprintf(` path="%s"`, p.Name)
		}

		// 始终包含 remote 属性，无论是项目自己定义的还是从 default 继承的
		xml += fmt.Sprintf(` remote="%s"`, p.Remote)

		// 始终包含 revision 属性，无论是项目自己定义的还是从 default 继承的
		xml += fmt.Sprintf(` revision="%s"`, p.Revision)

		// 其他属性按需包含
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

		// 添加项目的自定义属性，但排除内部使用的属性
		for k, v := range p.CustomAttrs {
			// 跳过以 "__" 开头的内部属性
			if !strings.HasPrefix(k, "__") {
				xml += fmt.Sprintf(` %s="%s"`, k, v)
			}
		}

		// 检查是否有copyfile或linkfile子元
		if len(p.Copyfiles) > 0 || len(p.Linkfiles) > 0 {
			xml += ">\n"

			// 添加copyfile子元
			for _, c := range p.Copyfiles {
				xml += fmt.Sprintf(`    <copyfile src="%s" dest="%s"`, c.Src, c.Dest)
				// 添加copyfile的自定义属
				for k, v := range c.CustomAttrs {
					xml += fmt.Sprintf(` %s="%s"`, k, v)
				}
				xml += " />\n"
			}

			// 添加linkfile子元
			for _, l := range p.Linkfiles {
				xml += fmt.Sprintf(`    <linkfile src="%s" dest="%s"`, l.Src, l.Dest)
				// 添加linkfile的自定义属
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
		if r.Path != "" {
			xml += fmt.Sprintf(` path="%s"`, r.Path)
		}
		if r.Optional {
			xml += ` optional="true"`
		}
		// 添加移除项目的自定义属性
		for k, v := range r.CustomAttrs {
			xml += fmt.Sprintf(` %s="%s"`, k, v)
		}
		xml += " />\n"
	}

	// 添加superproject（如果存在）
	if m.Superproject != nil {
		xml += fmt.Sprintf(`  <superproject name="%s"`, m.Superproject.Name)
		if m.Superproject.Remote != "" {
			xml += fmt.Sprintf(` remote="%s"`, m.Superproject.Remote)
		}
		if m.Superproject.Revision != "" {
			xml += fmt.Sprintf(` revision="%s"`, m.Superproject.Revision)
		}
		xml += " />\n"
	}

	// 关闭XML
	xml += "</manifest>\n"

	return xml, nil
}

func (m *Manifest) ParseFromBytes(data []byte, groups []string) error {
	if len(data) == 0 {
		return fmt.Errorf("manifest data is empty")
	}

	// 创建临时解析
	parser := NewParser()

	// 使用解析器解析数
	parsedManifest, err := parser.Parse(data, groups)
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

// 判断是否包含所有组
func containsAll(groups []string) bool {
	for _, group := range groups {
		if group == "all" {
			return true
		}
	}
	return false
}

// shouldIncludeProject 检查项目是否应该包含在指定的组中
// 支持否定表达式（以"-"开头的组表示排除）
func shouldIncludeProject(project Project, groups []string) bool {
	// 如果项目没有指定组，则默认为"default"
	if project.Groups == "" {
		project.Groups = "default"
	}

	// 解析项目的组
	projectGroups := strings.Split(project.Groups, ",")
	projectGroupSet := make(map[string]bool)
	for _, pg := range projectGroups {
		projectGroupSet[strings.TrimSpace(pg)] = true
	}

	// 分离包含和排除组
	var includeGroups []string
	var excludeGroups []string

	for _, group := range groups {
		group = strings.TrimSpace(group)
		if group == "" {
			continue
		}
		if strings.HasPrefix(group, "-") {
			// 排除组
			excludeGroups = append(excludeGroups, strings.TrimPrefix(group, "-"))
		} else {
			// 包含组
			includeGroups = append(includeGroups, group)
		}
	}

	// 检查是否包含"all"
	for _, group := range includeGroups {
		if group == "all" {
			// 即使包含all，也要检查排除组
			for _, excludeGroup := range excludeGroups {
				if projectGroupSet[excludeGroup] {
					return false
				}
			}
			return true
		}
	}

	// 检查是否有匹配的包含组
	hasMatch := false
	for _, projectGroup := range projectGroups {
		projectGroup = strings.TrimSpace(projectGroup)
		for _, group := range includeGroups {
			if projectGroup == group {
				hasMatch = true
				break
			}
		}
		if hasMatch {
			break
		}
	}

	// 如果没有匹配的包含组，返回false
	if !hasMatch && len(includeGroups) > 0 {
		return false
	}

	// 检查排除组
	for _, excludeGroup := range excludeGroups {
		if projectGroupSet[excludeGroup] {
			return false
		}
	}

	return hasMatch || len(includeGroups) == 0
}
