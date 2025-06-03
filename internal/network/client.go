package network

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/leopardxu/repo-go/internal/logger"
)

// Client 网络客户端
type Client struct {
	httpClient *http.Client
	userAgent  string
	retryCount int
	retryDelay time.Duration
}

// ClientOption 客户端选项函数类型
type ClientOption func(*Client)

// WithUserAgent 设置用户代理
func WithUserAgent(userAgent string) ClientOption {
	return func(c *Client) {
		c.userAgent = userAgent
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) ClientOption {
	return func(c *Client) {
		c.httpClient.Timeout = timeout
	}
}

// WithRetry 设置重试参数
func WithRetry(count int, delay time.Duration) ClientOption {
	return func(c *Client) {
		c.retryCount = count
		c.retryDelay = delay
	}
}

// NewClient 创建网络客户
func NewClient(options ...ClientOption) *Client {
	// 创建HTTP客户
	httpClient := &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
			DisableCompression: false,
			MaxIdleConns:       10,
			IdleConnTimeout:    30 * time.Second,
		},
	}

	client := &Client{
		httpClient: httpClient,
		userAgent:  "gogo-repo/1.0",
		retryCount: 3,
		retryDelay: 2 * time.Second,
	}

	// 应用选项
	for _, option := range options {
		option(client)
	}

	return client
}

// SetUserAgent 设置用户代理
func (c *Client) SetUserAgent(userAgent string) {
	c.userAgent = userAgent
}

// SetTimeout 设置超时时间
func (c *Client) SetTimeout(timeout time.Duration) {
	c.httpClient.Timeout = timeout
}

// Download 下载文件
func (c *Client) Download(url string, destPath string) error {
	logger.Info("开始下载文 %s", url)
	logger.Debug("下载目标路径: %s", destPath)

	// 确保目标目录存在
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		logger.Error("创建目标目录失败: %v", err)
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 实现重试逻辑
	var lastErr error
	for attempt := 0; attempt <= c.retryCount; attempt++ {
		if attempt > 0 {
			logger.Warn("下载重试 (%d/%d): %s", attempt, c.retryCount, url)
			time.Sleep(c.retryDelay)
		}

		err := c.doDownload(url, destPath)
		if err == nil {
			logger.Info("文件下载成功: %s", destPath)
			return nil
		}

		lastErr = err
		logger.Warn("下载失败 (%d/%d): %v", attempt+1, c.retryCount+1, err)
	}

	logger.Error("下载失败，已达到最大重试次 %v", lastErr)
	return fmt.Errorf("下载失败，已达到最大重试次 %w", lastErr)
}

// doDownload 执行实际的下载操
func (c *Client) doDownload(url string, destPath string) error {
	// 创建请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求
	req.Header.Set("User-Agent", c.userAgent)

	// 发送请
	logger.Debug("发送HTTP请求: %s", url)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("发送请求失 %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("服务器返回非成功状态码: %s", resp.Status)
	}

	// 创建目标文件
	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %w", err)
	}
	defer out.Close()

	// 复制数据
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return fmt.Errorf("写入文件失败: %w", err)
	}

	return nil
}

// Get 发送GET请求并返回响
func (c *Client) Get(url string, headers map[string]string) (*http.Response, error) {
	logger.Debug("发送GET请求: %s", url)

	// 创建请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求
	req.Header.Set("User-Agent", c.userAgent)
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// 发送请
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失 %w", err)
	}

	return resp, nil
}

// IsValidURL 检查URL是否有效
func IsValidURL(rawURL string) bool {
	_, err := url.ParseRequestURI(rawURL)
	return err == nil
}
