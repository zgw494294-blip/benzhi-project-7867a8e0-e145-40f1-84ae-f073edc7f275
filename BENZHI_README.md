# BENZHI_README

基于 Go 实现的舞台吊挂演出放行台 Web 项目，一款后端服务，已完整实现舞台吊挂演出放行台：以 SQLite 保存制作、吊挂方案、分析、排练、复核、冻结清单、凭据与审计记录。

## 项目说明
- 项目：benzhi-project-7867a8e0-e145-40f1-84ae-f073edc7f275
- 项目用途：已完整实现舞台吊挂演出放行台：以 SQLite 保存制作、吊挂方案、分析、排练、复核、冻结清单、凭据与审计记录。
- Go 工具链：`golang:1.23.0`
- 前端工具链：原生 HTML、CSS 和 JavaScript，由 Go 服务直接提供

## 标准构建、运行和测试命令
进入容器后执行：
cd '/app' && GOTOOLCHAIN=local go build ./...
cd /app && GOTOOLCHAIN=local go run ./cmd/stageclearance -selfcheck -addr=127.0.0.1:19081
cd '/app' && GOTOOLCHAIN=local go test ./...

## Docker 构建和进入容器
chmod +x build_benzhi_docker.sh
./build_benzhi_docker.sh benzhi-project-7867a8e0-e145-40f1-84ae-f073edc7f275-amd64 linux/amd64
./build_benzhi_docker.sh benzhi-project-7867a8e0-e145-40f1-84ae-f073edc7f275-arm64 linux/arm64
docker run -it benzhi-project-7867a8e0-e145-40f1-84ae-f073edc7f275-amd64:latest

## 题目验证命令
1. 预期退出码 0：`go test ./...`
2. 预期退出码 0：`go run ./cmd/stageclearance -selfcheck -addr=127.0.0.1:19081`
