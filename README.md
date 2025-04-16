# GoGo - Google Git-Repo Golang实现

GoGo是Google Git-Repo工具的Golang重新实现，用于管理多个Git仓库。

## 特性

- 高性能：通过Golang的并发模型提高多仓库操作效率
- 简单部署：单二进制文件分发，无依赖
- 增强功能：保持原有功能的同时增加新特性
- 跨平台：更好的Windows兼容性

## 安装

```bash
go install github.com/cix-code/gogo/cmd/repo@latest

# 初始化
repo init -u https://example.com/manifest.git

# 同步
repo sync

# 创建分支
repo start my-feature

# 查看状态
repo status
```