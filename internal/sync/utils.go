package sync

import (
	"github.com/cix-code/gogo/internal/git"
	"github.com/cix-code/gogo/internal/manifest"
	"github.com/cix-code/gogo/internal/project"
	"strings"
)

// contains 检查字符串切片是否包含指定字符串
func contains(slice []string, str string) bool {
	for _, item := range slice {
		if item == str {
			return true
		}
	}
	return false
}

// difference 返回在a中但不在b中的元素
func difference(a, b []string) []string {
	result := []string{}
	for _, item := range a {
		if !contains(b, item) {
			result = append(result, item)
		}
	}
	return result
}

// convertManifestProject 将 manifest.Project 转换为 project.Project
func convertManifestProject(mp *manifest.Project, gitRunner git.Runner) *project.Project {
	// 获取远程URL
	remoteURL, _ := mp.GetCustomAttr("__remote_url")
	
	// 解析组
	groups := []string{}
	if mp.Groups != "" {
		groups = strings.Split(mp.Groups, ",")
	}
	
	// 创建项目
	p := project.NewProject(
		mp.Name,
		mp.Path,
		mp.Remote,
		remoteURL,
		mp.Revision,
		groups,
		gitRunner,
	)
	
	// 转换 copyfiles
	for _, cf := range mp.Copyfiles {
		p.Copyfiles = append(p.Copyfiles, project.CopyFile{
			Src:  cf.Src,
			Dest: cf.Dest,
		})
	}
	
	// 转换 linkfiles
	for _, lf := range mp.Linkfiles {
		p.Linkfiles = append(p.Linkfiles, project.LinkFile{
			Src:  lf.Src,
			Dest: lf.Dest,
		})
	}
	
	// 设置其他字段
	p.NeedGC = mp.NeedGC
	
	return p
}