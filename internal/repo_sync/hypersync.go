package repo_sync

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	// "os" // Remove unused import
	// "path/filepath" // Remove unused import
	"strings"
	"time"

	"github.com/leopardxu/repo-go/internal/project"
)

// getHyperSyncProjects 获取需要通过HyperSync同步的项目
func (e *Engine) getHyperSyncProjects() ([]*project.Project, error) {
	if !e.options.HyperSync {
		return nil, nil
	}

	// 获取清单服务器
	manifestServer := e.manifest.ManifestServer
	if manifestServer == "" {
		return nil, fmt.Errorf("无法进行HyperSync: 清单中未定义清单服务器")
	}

	if !e.options.Quiet {
		fmt.Printf("使用清单服务器%s 进行HyperSync\n", manifestServer)
	}

	// 处理认证
	if !strings.Contains(manifestServer, "@") {
		username := e.options.ManifestServerUsername
		password := e.options.ManifestServerPassword

		if username != "" && password != "" {
			// 将用户名和密码添加到URL
			u, err := url.Parse(manifestServer)
			if err == nil {
				u.User = url.UserPassword(username, password)
				manifestServer = u.String()
			}
		}
	}

	// 获取分支名称
	branch := e.getBranch()

	// 构建请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 构建请求URL
	requestURL := fmt.Sprintf("%s/api/GetChangedProjects?branch=%s",
		manifestServer, url.QueryEscape(branch))

	// 发送请�?
	resp, err := client.Get(requestURL)
	if err != nil {
		return nil, fmt.Errorf("连接到清单服务器时出�? %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("清单服务器返回状�?%d", resp.StatusCode)
	}

	// 读取响应
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("从服务器读取响应时出�? %w", err)
	}

	// 解析响应
	var changedProjects []string
	if err := json.Unmarshal(data, &changedProjects); err != nil {
		return nil, fmt.Errorf("解析服务器响应时出错: %w", err)
	}

	// 过滤出已更改的项�?
	var hyperSyncProjects []*project.Project
	for _, project := range e.projects {
		if contains(changedProjects, project.Name) {
			hyperSyncProjects = append(hyperSyncProjects, project)
		}
	}

	if !e.options.Quiet {
		fmt.Printf("HyperSync: %d 个项目中�?%d 个已更改\n",
			len(hyperSyncProjects), len(e.projects))
	}

	return hyperSyncProjects, nil
}

// getChangedProjectsFromServer 从服务器获取已更改的项目
func (e *Engine) getChangedProjectsFromServer() ([]string, error) {
	// 获取清单服务器
	manifestServer := e.manifest.ManifestServer
	if manifestServer == "" {
		return nil, fmt.Errorf("无法获取已更改的项目: 清单中未定义清单服务器")
	}

	// 处理认证
	if !strings.Contains(manifestServer, "@") {
		username := e.options.ManifestServerUsername
		password := e.options.ManifestServerPassword

		if username != "" && password != "" {
			// 将用户名和密码添加到URL
			u, err := url.Parse(manifestServer)
			if err == nil {
				u.User = url.UserPassword(username, password)
				manifestServer = u.String()
			}
		}
	}

	// 获取分支名称
	branch := e.getBranch()

	// 构建请求
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// 构建请求URL
	requestURL := fmt.Sprintf("%s/api/GetChangedProjects?branch=%s",
		manifestServer, url.QueryEscape(branch))

	// 发送请�?
	resp, err := client.Get(requestURL)
	if err != nil {
		return nil, fmt.Errorf("连接到清单服务器时出�? %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("清单服务器返回状�?%d", resp.StatusCode)
	}

	// 读取响应
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("从服务器读取响应时出�? %w", err)
	}

	// 解析响应
	var changedProjects []string
	if err := json.Unmarshal(data, &changedProjects); err != nil {
		return nil, fmt.Errorf("解析服务器响应时出错: %w", err)
	}

	return changedProjects, nil
}
