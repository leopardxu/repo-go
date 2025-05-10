package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime" // 导入 runtime 包
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/sync/errgroup"
)

// Config 配置结构体
type Config struct {
	TargetDir        string
	LogFilePath      string
	MaxWatchDepth    int
	EventBufferSize  int
	EventBatchWindow time.Duration
}

// 默认配置
var defaultConfig = Config{
	TargetDir:        "/unisoc",
	LogFilePath:      "/var/log/unisoc_copy_audit.log",
	MaxWatchDepth:    8,
	EventBufferSize:  1000,
	EventBatchWindow: 500 * time.Millisecond,
}

func main() {
	// 解析命令行参数
	cfg := parseFlags()

	fmt.Printf("Starting monitoring with configuration:\n")
	fmt.Printf("  Target Directory: %s\n", cfg.TargetDir)
	fmt.Printf("  Log File: %s\n", cfg.LogFilePath)
	fmt.Printf("  Max Watch Depth: %d\n", cfg.MaxWatchDepth)
	fmt.Printf("  Event Buffer Size: %d\n", cfg.EventBufferSize)
	fmt.Printf("  Event Batch Window: %v\n", cfg.EventBatchWindow)

	// 初始化带缓冲的日志写入器
	logFile, err := os.OpenFile(cfg.LogFilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		log.Fatalf("Failed to open log file: %v", err)
	}
	defer logFile.Close()

	logWriter := bufio.NewWriterSize(logFile, 8192) // 8KB缓冲
	defer logWriter.Flush()

	// 创建事件通道和批处理机制
	eventChan := make(chan CopyEvent, cfg.EventBufferSize)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 启动日志写入worker
	go logWorker(ctx, logWriter, eventChan)

	// 创建有限制的目录监控器
	watcher, err := NewLimitedWatcher(cfg.MaxWatchDepth, cfg.TargetDir)
	if err != nil {
		log.Fatalf("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	// 使用errgroup管理goroutine
	g, ctx := errgroup.WithContext(ctx)

	// 启动事件处理worker
	g.Go(func() error {
		return eventProcessor(ctx, watcher, eventChan, cfg)
	})

	// 等待所有goroutine完成
	if err := g.Wait(); err != nil {
		log.Printf("Monitoring stopped with error: %v", err)
	}
}

// CopyEvent 拷贝/移动事件结构
type CopyEvent struct {
	Username  string
	Timestamp time.Time
	Command   string
	Source    string
	Target    string
	Operation string // "COPY" 或 "RENAME"
	// ProcessInfo string // 移除进程详细信息字段
}


// LimitedWatcher 带限制的监控器
type LimitedWatcher struct {
	*fsnotify.Watcher
	maxDepth int
	baseDir  string
	mu       sync.Mutex
	watched  map[string]struct{} // 已监控的目录集合
	// fileCache map[string]cachedFileInfo // 移除文件缓存
}

// NewLimitedWatcher 创建带限制的监控器
func NewLimitedWatcher(maxDepth int, baseDir string) (*LimitedWatcher, error) {
	baseWatcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	return &LimitedWatcher{
		Watcher:  baseWatcher,
		maxDepth: maxDepth,
		baseDir:  baseDir,
		watched:  make(map[string]struct{}),
		// fileCache: make(map[string]cachedFileInfo), // 移除初始化
	}, nil
}

// AddLimited 添加有限制的监控
func (lw *LimitedWatcher) AddLimited(path string) error {
	lw.mu.Lock()
	defer lw.mu.Unlock()

	// 检查是否已经监控
	if _, exists := lw.watched[path]; exists {
		return nil
	}

	// 检查深度
	relPath, err := filepath.Rel(lw.baseDir, path)
	if err != nil {
		return err
	}

	depth := strings.Count(relPath, string(os.PathSeparator))
	if depth > lw.maxDepth {
		return fmt.Errorf("path exceeds max depth: %s", path)
	}

	if err := lw.Watcher.Add(path); err != nil {
		return err
	}

	lw.watched[path] = struct{}{}
	return nil
}

// parseFlags 解析命令行参数
func parseFlags() Config {
	cfg := defaultConfig

	flag.StringVar(&cfg.TargetDir, "target", defaultConfig.TargetDir, "Target directory to monitor")
	flag.StringVar(&cfg.LogFilePath, "log", defaultConfig.LogFilePath, "Path to the log file")
	flag.IntVar(&cfg.MaxWatchDepth, "depth", defaultConfig.MaxWatchDepth, "Maximum directory depth to watch")
	flag.IntVar(&cfg.EventBufferSize, "buffer", defaultConfig.EventBufferSize, "Event buffer size")
	flag.DurationVar(&cfg.EventBatchWindow, "batch", defaultConfig.EventBatchWindow, "Event batch processing window")

	flag.Parse()

	// 确保目标目录是绝对路径
	if !filepath.IsAbs(cfg.TargetDir) {
		absPath, err := filepath.Abs(cfg.TargetDir)
		if err == nil {
			cfg.TargetDir = absPath
		}
	}

	return cfg
}

// 在 eventProcessor 中移除 fileCache 的使用
func eventProcessor(ctx context.Context, watcher *LimitedWatcher, eventChan chan<- CopyEvent, cfg Config) error {
	var (
		batch      []fsnotify.Event
		batchTimer *time.Timer
		batchMu    sync.Mutex
	)

	processBatch := func() {
		batchMu.Lock()
		defer batchMu.Unlock()

		if len(batch) == 0 {
			return
		}

		for _, event := range batch {
			// 处理事件
			if event.Op&fsnotify.Create == fsnotify.Create {
				fileInfo, err := os.Stat(event.Name)
				if err != nil {
					log.Printf("Failed to stat created item %s (may be transient): %v", event.Name, err)
					continue
				}

				if fileInfo.IsDir() {
					// 如果是新创建的目录，尝试添加到监控中
					log.Printf("Directory created: %s. Adding to watch.", event.Name)
					if err := watcher.AddLimited(event.Name); err != nil {
						log.Printf("Failed to add watch for new directory %s: %v", event.Name, err)
					}
					// 不记录目录创建为拷贝/移动操作到主日志
				} else {
					// 文件创建事件 - 不再记录到 eventChan
					log.Printf("File created: %s (Not logged as operation)", event.Name)
					// recordCopyOperation(event.Name, eventChan) // <--- 移除此行
				}

			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				// 文件或目录被删除/移出
				log.Printf("Removed/Moved Out event detected (logged as REMOVE): %s", event.Name) // 保留基础日志用于调试
				// 记录 Remove 事件到主日志
				recordRemoveOperation(event.Name, eventChan) // <--- 新增调用

			} else if event.Op&fsnotify.Write == fsnotify.Write {
				// 文件被写入 - 不记录到 eventChan
				// log.Printf("Modified: %s", event.Name)

			} else if event.Op&fsnotify.Rename == fsnotify.Rename {
				// 文件或目录被重命名/移动
				// event.Name 通常是旧名称/路径
				// 我们假设这可能是文件夹移动，并记录它
				log.Printf("Renamed/Moved event detected (logged as RENAME): %s", event.Name)
				recordRenameOperation(event.Name, eventChan) // 使用旧名称作为源

			} else if event.Op&fsnotify.Chmod == fsnotify.Chmod {
				// 文件权限发生变化 - 不记录到 eventChan
				// log.Printf("Chmod: %s", event.Name)
			}
		}

		batch = batch[:0] // 清空批次
		if batchTimer != nil {
			batchTimer.Stop()
			batchTimer = nil
		}
	}

	// 启动时先扫描并添加现有目录
	err := filepath.WalkDir(cfg.TargetDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// 忽略权限错误等，但记录日志
			log.Printf("Error accessing path %q: %v\n", path, err)
			return filepath.SkipDir // 如果访问目录出错，跳过它
		}
		if d.IsDir() {
			// 尝试添加目录到监控
			if addErr := watcher.AddLimited(path); addErr != nil {
				log.Printf("Failed to add initial watch for %s: %v", path, addErr)
				// 如果添加失败（例如深度超出），则跳过此目录及其子目录
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("Error during initial directory walk: %v", err)
		// 根据情况决定是否需要退出
	}
	fmt.Printf("Initial directory scan complete. Watching for events...\n")


	for {
		select {
		case <-ctx.Done():
			processBatch() // 处理剩余事件
			return ctx.Err()

		case event, ok := <-watcher.Events:
			if !ok {
				processBatch() // 处理剩余事件
				return nil
			}

			// 过滤掉临时文件或不关心的事件（如果需要）
			// if strings.HasSuffix(event.Name, "~") { continue }

			batchMu.Lock()
			batch = append(batch, event)
			// 只有在计时器未激活时才启动新的计时器
			if batchTimer == nil && len(batch) > 0 {
				batchTimer = time.AfterFunc(cfg.EventBatchWindow, processBatch)
			}
			batchMu.Unlock()

		case err, ok := <-watcher.Errors:
			if !ok {
				processBatch() // 处理剩余事件
				return nil
			}
			log.Printf("Watcher error: %v", err)
		}
	}
}

// recordCopyOperation 记录文件创建事件（视为拷贝）
func recordCopyOperation(target string, eventChan chan<- CopyEvent) {
	currentUser, _ := user.Current()
	username := "unknown"
	if currentUser != nil {
		username = currentUser.Username
	}

	// 警告：获取的是监控进程本身的PID，而非触发事件的进程PID
	pid := os.Getpid()
	cmd := getCommandInfo(pid)
	// procInfo := getProcessDetails(pid) // Remove this line

	eventChan <- CopyEvent{
		Username:    username,
		Timestamp:   time.Now(),
		Command:     fmt.Sprintf("%s (monitor process, not trigger)", cmd), // 标注PID来源
		Target:      target,
		Operation:   "COPY",
		// ProcessInfo: procInfo, // Remove this line
	}
}

// recordRemoveOperation 记录文件/目录移除事件
func recordRemoveOperation(source string, eventChan chan<- CopyEvent) {
	currentUser, _ := user.Current()
	username := "unknown"
	if currentUser != nil {
		username = currentUser.Username
	}

	// 警告：获取的是监控进程本身的PID，而非触发事件的进程PID
	pid := os.Getpid()
	cmd := getCommandInfo(pid)

	eventChan <- CopyEvent{
		Username:  username,
		Timestamp: time.Now(),
		Command:   fmt.Sprintf("%s (monitor process, not trigger)", cmd), // 标注PID来源
		Source:    source,                                                // 被移除的路径
		Target:    "",                                                    // Remove 事件没有目标路径
		Operation: "REMOVE",                                              // 标记为移除操作
	}
}

// recordRenameOperation 记录文件/目录重命名事件（视为移动）
func recordRenameOperation(source string, eventChan chan<- CopyEvent) {
	currentUser, _ := user.Current()
	username := "unknown"
	if currentUser != nil {
		username = currentUser.Username
	}

	// 警告：获取的是监控进程本身的PID，而非触发事件的进程PID
	pid := os.Getpid()
	cmd := getCommandInfo(pid)
	// procInfo := getProcessDetails(pid) // Remove this line

	eventChan <- CopyEvent{
		Username:    username,
		Timestamp:   time.Now(),
		Command:     fmt.Sprintf("%s (monitor process, not trigger)", cmd), // 标注PID来源
		Source:      source,
		Operation:   "RENAME",
		// ProcessInfo: procInfo, // Remove this line
	}
}

// logWorker 从通道读取事件并写入日志文件
func logWorker(ctx context.Context, writer *bufio.Writer, eventChan <-chan CopyEvent) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			if err := writer.Flush(); err != nil {
				log.Printf("Failed to flush log buffer on exit: %v", err)
			}
			return

		case event := <-eventChan:
			var logEntry string
			switch event.Operation {
			case "RENAME": // 处理 RENAME 操作 (视为文件夹移动)
				logEntry = fmt.Sprintf("FOLDER RENAME/MOVE - User: %s, Time: %s, Command: %s, Source(Old Path): %s, Target(New Path): %s\n",
					event.Username,
					event.Timestamp.Format(time.RFC3339),
					event.Command,
					event.Source,
					event.Target) // Target 标记为 unknown
			case "REMOVE": // 处理 REMOVE 操作 (删除或移出) <--- 新增 Case
				logEntry = fmt.Sprintf("FOLDER REMOVE/MOVE_OUT - User: %s, Time: %s, Command: %s, Path: %s (Note: Cannot distinguish delete from move-out)\n",
					event.Username,
					event.Timestamp.Format(time.RFC3339),
					event.Command,
					event.Source) // 只记录被移除的路径
			default:
				// 保留未知操作的日志，以防万一，但也移除 Process Info
				logEntry = fmt.Sprintf("UNKNOWN OPERATION (%s) - User: %s, Time: %s, Command: %s, Source: %s, Target: %s\n",
					event.Operation,
					event.Username,
					event.Timestamp.Format(time.RFC3339),
					event.Command,
					event.Source,
					event.Target)
			}

			if _, err := writer.WriteString(logEntry); err != nil {
				log.Printf("Failed to write log: %v", err)
			}

		case <-ticker.C:
			if err := writer.Flush(); err != nil {
				log.Printf("Failed to flush log buffer: %v", err)
			}
		}
	}
}

func getCommandInfo(pid int) string {
	var cmd *exec.Cmd
	var output []byte
	var err error

	switch runtime.GOOS {
	case "windows":
		// 尝试直接调用 tasklist
		cmd = exec.Command("tasklist", "/fi", fmt.Sprintf("PID eq %d", pid), "/nh", "/fo", "CSV")
		output, err = cmd.CombinedOutput()
		// 如果找不到 tasklist，尝试使用完整路径 (常见于 PATH 配置问题)
		if err != nil && strings.Contains(err.Error(), "executable file not found") {
			cmd = exec.Command(`C:\Windows\System32\tasklist.exe`, "/fi", fmt.Sprintf("PID eq %d", pid), "/nh", "/fo", "CSV")
			output, err = cmd.CombinedOutput()
		}
	case "linux", "darwin": // macOS
		cmd = exec.Command("ps", "-p", fmt.Sprintf("%d", pid), "-o", "comm=") // 使用 comm= 获取命令名
		output, err = cmd.CombinedOutput()
	default:
		return fmt.Sprintf("unknown (unsupported OS: %s)", runtime.GOOS)
	}

	if err != nil {
		return fmt.Sprintf("unknown (error getting command: %v)", err)
	}

	sOutput := strings.TrimSpace(string(output))

	if runtime.GOOS == "windows" {
		// 解析 tasklist CSV 输出
		parts := strings.Split(sOutput, ",")
		if len(parts) > 0 {
			commandName := strings.Trim(parts[0], "\"")
			return commandName
		}
		return "unknown (failed to parse tasklist output)"
	}
	// 对于 ps 命令，输出通常就是命令名
	return sOutput

}

// Helper function (placeholder, needs OS-specific implementation)
func getProcessDetails(pid int) string {
	// This function is currently undefined and its usage is removed.
	// If needed in the future, implement OS-specific logic here.
	// e.g., using psutil or similar libraries/system calls.
	return fmt.Sprintf("Details for PID %d (not implemented)", pid)
}