@echo off
echo 正在运行测试...

REM 运行单元测试
go test -v ./...

REM 运行覆盖率测试
REM go test -coverprofile=coverage.txt -covermode=atomic ./...

echo 测试完成！