package hook

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
	
	"github.com/leopardxu/repo-go/internal/logger"
)

// 包级别的日志记录�?
var log logger.Logger = &logger.DefaultLogger{}

// SetLogger 设置包级别的日志记录�?
func SetLogger(l logger.Logger) {
	if l != nil {
		log = l
	}
}

// HookError 表示hook操作过程中的错误
type HookError struct {
	Op   string // 操作名称
	Path string // 文件路径
	Err  error  // 原始错误
}

func (e *HookError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("hook error: %s failed for '%s': %v", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("hook error: %s failed: %v", e.Op, e.Err)
}

func (e *HookError) Unwrap() error {
	return e.Err
}

// 文件操作的重试配�?
const (
	maxRetries = 3
	retryDelay = 100 * time.Millisecond
)

// 预定义的hook模板
var hookTemplates = map[string]string{
	"pre-commit": `#!/bin/bash
# try to find full path of this APP
set -o pipefail
__APP_FULLPATH="${BASH_SOURCE[0]}";
if [[ x"$__APP_FULLPATH" =~ / ]]; then
    # / is found in path, using readlink to expend to abslute path
    APP_FULLPATH=$(readlink -f "$__APP_FULLPATH");
else
    # / not found in path, try to find with which command
    APP_FULLPATH=$(which "$__APP_FULLPATH");
    # not in $PATH, try to find in current directory
    if [[ -z "$APP_FULLPATH" ]]; then
        APP_FULLPATH="$(pwd)/${__APP_FULLPATH}";
    fi
fi
if [[ ! -e "${APP_FULLPATH}" ]]; then
    echo "Cannot determine location of this script";
    exit 1;
fi

APP_DIR=$(dirname "${APP_FULLPATH}")
APP_NAME=$(basename "${APP_FULLPATH}")
HOOK_DIR=$(readlink -f "${APP_DIR}/../cixtools/hooks")
if [[ ! -e "${HOOK_DIR}" ]]; then
    echo "No hook found, ignore pre-commit check" >&2;
    exit 0;
fi

WORKTREE=$(git rev-parse --show-toplevel)
if [[ -z "${WORKTREE}" ]]; then
    echo "Error: not a git repository, quit $WORKTREE" >&2;
    exit 1;
fi
GIT_DIR=$(git rev-parse --git-dir)
if [[ -z "${GIT_DIR}" ]]; then
    echo "Error: not a git repository, quit $GIT_DIR" >&2;
    exit 1;
fi

if [[ ! -e "${WORKTREE}/.cix-ciconfig" ]]; then
    exit 0;  # no ci config found, skip silently
fi

set -e
cat "${WORKTREE}/.cix-ciconfig" |
grep '^pre-commit[[:space:]]*=' |
sed -e 's#^pre-commit[[:space:]]*=[[:space:]]*##g'|
while read script_name; do
    if [[ -z $(echo "${script_name}"|grep '^["'\'']?/') ]]; then
        echo eval "\"${HOOK_DIR}/\"${script_name} \"${WORKTREE}\" \"${GIT_DIR}\"" >&2;  # relative path to hooks/tools/
        eval "\"${HOOK_DIR}/\"${script_name} \"${WORKTREE}\" \"${GIT_DIR}\"";  # relative path to hooks/tools/
        if [[ $? -ne 0 ]]; then
            exit $?;  # quit if any hook failed
        fi
    else
        echo eval "${script_name} \"${WORKTREE}\" \"${GIT_DIR}\"" >&2;  # absolute path specified
        eval "${script_name} \"${WORKTREE}\" \"${GIT_DIR}\"";  # absolute path specified
        if [[ $? -ne 0 ]]; then
            exit $?;  # quit if any hook failed
        fi
    fi
done
`,
	"commit-msg": `#!/bin/sh
# From Gerrit Code Review 3.1.3
#
# Part of Gerrit Code Review (https://www.gerritcodereview.com/)
#
# Copyright (C) 2009 The Android Open Source Project
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# avoid [[ which is not POSIX sh.
if test "$#" != 1 ; then
  echo "$0 requires an argument."
  exit 1
fi

if test ! -f "$1" ; then
  echo "file does not exist: $1"
  exit 1
fi

# Do not create a change id if requested
if test "false" = "$(git config --bool --get gerrit.createChangeId)" ; then
  exit 0
fi

# $RANDOM will be undefined if not using bash, so don't use set -u
random=$( (whoami ; hostname ; date; cat $1 ; echo $RANDOM) | git hash-object --stdin)
dest="$1.tmp.${random}"

trap 'rm -f "${dest}"' EXIT

if ! git stripspace --strip-comments < "$1" > "${dest}" ; then
   echo "cannot strip comments from $1"
   exit 1
fi

if test ! -s "${dest}" ; then
  echo "file is empty: $1"
  exit 1
fi

# Avoid the --in-place option which only appeared in Git 2.8
# Avoid the --if-exists option which only appeared in Git 2.15
if ! git -c trailer.ifexists=doNothing interpret-trailers \
      --trailer "Change-Id: I${random}" < "$1" > "${dest}" ; then
  echo "cannot insert change-id line in $1"
  exit 1
fi

if ! mv "${dest}" "$1" ; then
  echo "cannot mv ${dest} to $1"
  exit 1
fi
`,
	"pre-auto-gc": `#!/bin/sh
#
# An example hook script to verify if you are on battery, in case you
# are running Windows, Linux or OS X. Called by git-gc --auto with no
# arguments. The hook should exit with non-zero status after issuing an
# appropriate message if it wants to stop the auto repacking.

# This program is free software; you can redistribute it and/or modify
# it under the terms of the GNU General Public License as published by
# the Free Software Foundation; either version 2 of the License, or
# (at your option) any later version.
#
# This program is distributed in the hope that it will be useful,
# but WITHOUT ANY WARRANTY; without even the implied warranty of
# MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
# GNU General Public License for more details.
#
# You should have received a copy of the GNU General Public License
# along with this program; if not, write to the Free Software
# Foundation, Inc., 59 Temple Place, Suite 330, Boston, MA  02111-1307  USA

if uname -s | grep -q "_NT-"
then
        if test -x $SYSTEMROOT/System32/Wbem/wmic
        then
                STATUS=$(wmic path win32_battery get batterystatus /format:list | tr -d '\r\n')
                [ "$STATUS" = "BatteryStatus=2" ] && exit 0 || exit 1
        fi
        exit 0
fi

if test -x /sbin/on_ac_power && (/sbin/on_ac_power;test $? -ne 1)
then
        exit 0
elif test "$(cat /sys/class/power_supply/AC/online 2>/dev/null)" = 1
then
        exit 0
elif grep -q 'on-line' /proc/acpi/ac_adapter/AC/state 2>/dev/null
then
        exit 0
elif grep -q '0x01$' /proc/apm 2>/dev/null
then
        exit 0
elif grep -q "AC Power \+: 1" /proc/pmu/info 2>/dev/null
then
        exit 0
elif test -x /usr/bin/pmset && /usr/bin/pmset -g batt |
        grep -q "drawing from 'AC Power'"
then
        exit 0
elif test -d /sys/bus/acpi/drivers/battery && test 0 = \
  "$(find /sys/bus/acpi/drivers/battery/ -type l | wc -l)";
then
        # No battery exists.
        exit 0
fi

echo "Auto packing deferred; not on AC"
exit 1
`,
}

// InitHooks 初始化Git hooks
func InitHooks(repoDir string) error {
	log.Debug("初始化Git hooks: %s", repoDir)
	
	// 创建hooks目录
	hooksDir := filepath.Join(repoDir, ".repo", "hooks")
	if err := os.MkdirAll(hooksDir, 0755); err != nil {
		log.Error("创建hooks目录失败: %v", err)
		return &HookError{
			Op:   "init_hooks",
			Path: hooksDir,
			Err:  err,
		}
	}

	// 使用并发创建hook文件
	var wg sync.WaitGroup
	errorCh := make(chan error, len(hookTemplates))
	
	for hookName, hookContent := range hookTemplates {
		wg.Add(1)
		go func(name, content string) {
			defer wg.Done()
			
			hookPath := filepath.Join(hooksDir, name)
			
			// 检查文件是否已存在且内容相�?
			if fileExists(hookPath) {
				existingContent, err := os.ReadFile(hookPath)
				if err == nil && string(existingContent) == content {
					log.Debug("Hook文件已存在且内容相同，跳过创�? %s", hookPath)
					return
				}
			}
			
			// 使用重试机制写入文件
			var err error
			for i := 0; i < maxRetries; i++ {
				err = os.WriteFile(hookPath, []byte(content), 0755)
				if err == nil {
					log.Debug("成功创建hook文件: %s", hookPath)
					break
				}
				
				log.Debug("创建hook文件失败，尝试重�?(%d/%d): %v", i+1, maxRetries, err)
				time.Sleep(retryDelay)
			}
			
			if err != nil {
				errorCh <- &HookError{
					Op:   "create_hook",
					Path: hookPath,
					Err:  err,
				}
			}
		}(hookName, hookContent)
	}
	
	// 等待所有goroutine完成
	wg.Wait()
	close(errorCh)
	
	// 检查是否有错误
	select {
	case err := <-errorCh:
		return err
	default:
		log.Info("成功初始化所有Git hooks")
		return nil
	}
}

// CreateRepoGitConfig 创建repo.git配置文件
func CreateRepoGitConfig(repoDir string) error {
	log.Debug("创建repo.git配置文件: %s", repoDir)
	
	// 创建.repo/repo.git文件
	configPath := filepath.Join(repoDir, ".repo", "repo.git")
	
	// 检查文件是否已存在
	if fileExists(configPath) {
		log.Debug("repo.git配置文件已存�? %s", configPath)
		return nil
	}
	
	content := `[core]
	repositoryformatversion = 0
	filemode = true
	bare = true
[filter "lfs"]
	clean = git-lfs clean -- %f
	smudge = git-lfs smudge -- %f
	process = git-lfs filter-process
	required = true
`

	// 确保目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Error("创建配置目录失败: %v", err)
		return &HookError{
			Op:   "create_config_dir",
			Path: configDir,
			Err:  err,
		}
	}

	// 使用重试机制写入文件
	var err error
	for i := 0; i < maxRetries; i++ {
		err = os.WriteFile(configPath, []byte(content), 0644)
		if err == nil {
			log.Info("成功创建repo.git配置文件: %s", configPath)
			break
		}
		
		log.Debug("创建repo.git配置文件失败，尝试重�?(%d/%d): %v", i+1, maxRetries, err)
		time.Sleep(retryDelay)
	}
	
	if err != nil {
		return &HookError{
			Op:   "create_repo_git_config",
			Path: configPath,
			Err:  err,
		}
	}

	return nil
}

// CreateRepoGitconfig 创建repo.gitconfig文件
func CreateRepoGitconfig(repoDir string) error {
	log.Debug("创建repo.gitconfig文件: %s", repoDir)
	
	// 创建.repo/repo.gitconfig文件
	configPath := filepath.Join(repoDir, ".repo", "repo.gitconfig")
	
	// 检查文件是否已存在
	if fileExists(configPath) {
		log.Debug("repo.gitconfig文件已存�? %s", configPath)
		return nil
	}
	
	content := `[filter "lfs"]
	clean = git-lfs clean -- %f
	smudge = git-lfs smudge -- %f
	process = git-lfs filter-process
	required = true
`

	// 确保目录存在
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		log.Error("创建配置目录失败: %v", err)
		return &HookError{
			Op:   "create_config_dir",
			Path: configDir,
			Err:  err,
		}
	}

	// 使用重试机制写入文件
	var err error
	for i := 0; i < maxRetries; i++ {
		err = os.WriteFile(configPath, []byte(content), 0644)
		if err == nil {
			log.Info("成功创建repo.gitconfig文件: %s", configPath)
			break
		}
		
		log.Debug("创建repo.gitconfig文件失败，尝试重�?(%d/%d): %v", i+1, maxRetries, err)
		time.Sleep(retryDelay)
	}
	
	if err != nil {
		return &HookError{
			Op:   "create_repo_gitconfig",
			Path: configPath,
			Err:  err,
		}
	}

	return nil
}

// LinkHooks 将hooks链接到项目目�?
func LinkHooks(projectDir string, hooksDir string) error {
	log.Debug("链接hooks到项目目�? %s -> %s", hooksDir, projectDir)
	
	// 检查项目目录是否存�?
	if !fileExists(projectDir) {
		log.Error("项目目录不存�? %s", projectDir)
		return &HookError{
			Op:   "link_hooks",
			Path: projectDir,
			Err:  fmt.Errorf("project directory does not exist"),
		}
	}
	
	// 检查hooks目录是否存在
	if !fileExists(hooksDir) {
		log.Error("hooks目录不存�? %s", hooksDir)
		return &HookError{
			Op:   "link_hooks",
			Path: hooksDir,
			Err:  fmt.Errorf("hooks directory does not exist"),
		}
	}
	
	// 确保项目�?git/hooks目录存在
	projectHooksDir := filepath.Join(projectDir, ".git", "hooks")
	if err := os.MkdirAll(projectHooksDir, 0755); err != nil {
		log.Error("创建项目hooks目录失败: %v", err)
		return &HookError{
			Op:   "create_project_hooks_dir",
			Path: projectHooksDir,
			Err:  err,
		}
	}

	// 遍历hooks目录中的所有文�?
	entries, err := os.ReadDir(hooksDir)
	if err != nil {
		log.Error("读取hooks目录失败: %v", err)
		return &HookError{
			Op:   "read_hooks_dir",
			Path: hooksDir,
			Err:  err,
		}
	}

	// 使用并发处理hook文件
	var wg sync.WaitGroup
	errorCh := make(chan error, len(entries))
	
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		
		wg.Add(1)
		go func(e os.DirEntry) {
			defer wg.Done()
			
			// 源文件和目标文件路径
			srcPath := filepath.Join(hooksDir, e.Name())
			dstPath := filepath.Join(projectHooksDir, e.Name())
			
			// 尝试使用符号链接（在支持的系统上�?
			if trySymlink(srcPath, dstPath) {
				log.Debug("成功创建符号链接: %s -> %s", dstPath, srcPath)
				return
			}

			// 如果目标文件已存在，先删�?
			if fileExists(dstPath) {
				if err := os.Remove(dstPath); err != nil {
					errorCh <- &HookError{
						Op:   "remove_existing_hook",
						Path: dstPath,
						Err:  err,
					}
					return
				}
			}

			// 读取源文件内�?
			srcContent, err := os.ReadFile(srcPath)
			if err != nil {
				errorCh <- &HookError{
					Op:   "read_hook_file",
					Path: srcPath,
					Err:  err,
				}
				return
			}

			// 使用重试机制写入文件
			for i := 0; i < maxRetries; i++ {
				err = os.WriteFile(dstPath, srcContent, 0755)
				if err == nil {
					log.Debug("成功复制hook文件: %s -> %s", srcPath, dstPath)
					break
				}
				
				log.Debug("复制hook文件失败，尝试重�?(%d/%d): %v", i+1, maxRetries, err)
				time.Sleep(retryDelay)
			}
			
			if err != nil {
				errorCh <- &HookError{
					Op:   "write_hook_file",
					Path: dstPath,
					Err:  err,
				}
			}
		}(entry)
	}
	
	// 等待所有goroutine完成
	wg.Wait()
	close(errorCh)
	
	// 检查是否有错误
	select {
	case err := <-errorCh:
		return err
	default:
		log.Info("成功链接所有hooks到项目目�? %s", projectDir)
		return nil
	}
}

// fileExists 检查文件或目录是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// trySymlink 尝试创建符号链接，如果不支持则返回false
func trySymlink(src, dst string) bool {
	// 如果目标文件已存在，先删�?
	if fileExists(dst) {
		if err := os.Remove(dst); err != nil {
			return false
		}
	}
	
	// 尝试创建符号链接
	err := os.Symlink(src, dst)
	return err == nil
}
