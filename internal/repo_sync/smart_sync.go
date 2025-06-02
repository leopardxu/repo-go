package repo_sync

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	// "github.com/leopardxu/repo-go/internal/git" // Keep this commented if unused
	// "github.com/leopardxu/repo-go/internal/project" // Uncomment this import
)

// handleSmartSync 处理智能同步
func (e *Engine) handleSmartSync() error {
	if e.manifest.ManifestServer == "" {
		return errors.New("无法进行智能同步: 清单中未定义清单服务�?)
	}
	
	manifestServer := e.manifest.ManifestServer
	if !e.options.Quiet {
		fmt.Printf("使用清单服务�?%s\n", manifestServer)
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
	
	// 创建临时清单文件
	smartSyncManifestPath := filepath.Join(e.manifest.RepoDir, "smart-sync-manifest.xml")
	
	// 获取分支名称
	branch := e.getBranch()
	
	// 构建请求
	client := &http.Client{
		Timeout: e.options.HTTPTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
		},
	}
	
	var requestURL string
	if e.options.SmartSync {
		// 使用环境变量确定目标
		target := os.Getenv("SYNC_TARGET")
		if target == "" {
			product := os.Getenv("TARGET_PRODUCT")
			variant := os.Getenv("TARGET_BUILD_VARIANT")
			if product != "" && variant != "" {
				target = fmt.Sprintf("%s-%s", product, variant)
			}
		}
		
		if target != "" {
			requestURL = fmt.Sprintf("%s/api/GetApprovedManifest?branch=%s&target=%s", 
				manifestServer, url.QueryEscape(branch), url.QueryEscape(target))
		} else {
			requestURL = fmt.Sprintf("%s/api/GetApprovedManifest?branch=%s", 
				manifestServer, url.QueryEscape(branch))
		}
	} else {
		requestURL = fmt.Sprintf("%s/api/GetManifest?tag=%s", 
			manifestServer, url.QueryEscape(e.options.SmartTag))
	}
	
	// 发送请求，带重试机�?
	var resp *http.Response
	var err error
	maxRetries := 3
	for i := 0; i < maxRetries; i++ {
		resp, err = client.Get(requestURL)
		if err == nil {
			break
		}
		if i < maxRetries-1 {
			time.Sleep(time.Second * time.Duration(i+1))
		}
	}
	if err != nil {
		return fmt.Errorf("连接到清单服务器时出�?尝试%d�?: %w", maxRetries, err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("清单服务器返回状�?%d", resp.StatusCode)
	}
	
	// 读取响应
	manifestStr, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("从服务器读取清单时出�? %w", err)
	}
	
	// 使用内存缓存处理清单
	e.manifestCache = manifestStr
	
	// 重新加载清单
	if err := e.reloadManifestFromCache(); err != nil {
		return err
	}
	
	// 可选：写入临时文件用于调试
	if e.options.Debug {
		if err := os.WriteFile(smartSyncManifestPath, manifestStr, 0644); err != nil {
			return fmt.Errorf("将清单写�?%s 时出�? %w", smartSyncManifestPath, err)
		}
	}
	
	return nil
}

// getBranch 获取当前分支名称
func (e *Engine) getBranch() string {
	p := e.manifest.ManifestProject
	branch, err := p.GetBranch()
	if err != nil {
		return ""
	}
	if strings.HasPrefix(branch, "refs/heads/") {
		branch = branch[len("refs/heads/"):]
	}
	return branch
}
