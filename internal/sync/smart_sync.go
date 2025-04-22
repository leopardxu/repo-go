package sync

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

	// "github.com/cix-code/gogo/internal/git" // Keep this commented if unused
	// "github.com/cix-code/gogo/internal/project" // Uncomment this import
)

// handleSmartSync 处理智能同步
func (e *Engine) handleSmartSync() error {
	if e.manifest.ManifestServer == "" {
		return errors.New("无法进行智能同步: 清单中未定义清单服务器")
	}
	
	manifestServer := e.manifest.ManifestServer
	if !e.options.Quiet {
		fmt.Printf("使用清单服务器 %s\n", manifestServer)
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
		Timeout: 30 * time.Second,
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
	
	// 发送请求
	resp, err := client.Get(requestURL)
	if err != nil {
		return fmt.Errorf("连接到清单服务器时出错: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("清单服务器返回状态 %d", resp.StatusCode)
	}
	
	// 读取响应
	manifestStr, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("从服务器读取清单时出错: %w", err)
	}
	
	// 写入临时文件
	if err := os.WriteFile(smartSyncManifestPath, manifestStr, 0644); err != nil {
		return fmt.Errorf("将清单写入 %s 时出错: %w", smartSyncManifestPath, err)
	}
	
	// 重新加载清单
	manifestName := filepath.Base(smartSyncManifestPath)
	if err := e.reloadManifest(manifestName, true); err != nil {
		return err
	}
	
	return nil
}

// getBranch 获取当前分支名称
func (e *Engine) getBranch() string {
	p := e.manifest.ManifestProject
	branch := p.GetBranch()
	if strings.HasPrefix(branch, "refs/heads/") {
		branch = branch[len("refs/heads/"):]
	}
	return branch
}