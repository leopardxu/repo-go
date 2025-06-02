@echo off
echo 正在初始化GoGo项目...

REM 创建目录结构
mkdir d:\cix-code\gogo\cmd\repo\commands
mkdir d:\cix-code\gogo\internal\config
mkdir d:\cix-code\gogo\internal\git
mkdir d:\cix-code\gogo\internal\hook
mkdir d:\cix-code\gogo\internal\manifest
mkdir d:\cix-code\gogo\internal\network
mkdir d:\cix-code\gogo\internal\project
mkdir d:\cix-code\gogo\internal\sync
mkdir d:\cix-code\gogo\pkg\logger
mkdir d:\cix-code\gogo\pkg\util
mkdir d:\cix-code\gogo\docs\api
mkdir d:\cix-code\gogo\test\fixtures
mkdir d:\cix-code\gogo\test\integration
mkdir d:\cix-code\gogo\scripts

REM 初始化Go模块
cd d:\cix-code\gogo
go mod init github.com/leopardxu/repo-go

REM 安装依赖
go get github.com/spf13/cobra
go get github.com/spf13/viper
go get golang.org/x/sync/errgroup

echo 项目初始化完成！