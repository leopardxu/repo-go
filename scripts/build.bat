@echo off
echo 正在构建GoGo...

REM 设置版本信息
set VERSION=0.1.0
set COMMIT=%RANDOM%%RANDOM%
set BUILD_DATE=%DATE% %TIME%

REM 创建bin目录
if not exist bin mkdir bin

REM 构建Windows版本
echo 构建Windows版本...
go build -ldflags="-s -w -X main.version=%VERSION% -X main.commit=%COMMIT% -X 'main.date=%BUILD_DATE%' -buildmode=pie" -gcflags="-trimpath" -asmflags="-trimpath" -o bin\repo.exe .\cmd\repo

REM 如果需要构建其他平台，取消下面的注释
echo 构建Linux版本...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -ldflags="-s -w -X main.version=%VERSION% -X main.commit=%COMMIT% -X 'main.date=%BUILD_DATE%' -buildmode=pie" -gcflags="-trimpath" -asmflags="-trimpath" -o bin\repo-linux .\cmd\repo

REM echo 构建macOS版本...
REM set GOOS=darwin
REM set GOARCH=amd64
REM go build -ldflags="-s -w -X main.version=%VERSION% -X main.commit=%COMMIT% -X 'main.date=%BUILD_DATE%'" -o bin\repo-macos .\cmd\repo

echo 构建完成