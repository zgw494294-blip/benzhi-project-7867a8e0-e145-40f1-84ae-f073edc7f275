package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"stageclearance/internal/analyzer"
	"stageclearance/internal/httpui"
	"stageclearance/internal/store"
	"stageclearance/internal/workflow"
)

func main() {
	if err := run(); err != nil {
		log.Printf("启动失败：%v", err)
		os.Exit(1)
	}
}

func run() error {
	requestedAddr := flag.String("addr", "", "监听地址（仅允许回环地址）")
	databasePath := flag.String("db", "stageclearance.db", "SQLite 数据库路径")
	selfcheck := flag.Bool("selfcheck", false, "经真实 HTTP 执行有界主流程自检")
	flag.Parse()
	addr, err := resolveAddress(*requestedAddr, os.Getenv("PORT"))
	if err != nil {
		return err
	}
	if *selfcheck {
		return runSelfcheck(addr)
	}
	applicationStore, err := store.Open(*databasePath)
	if err != nil {
		return fmt.Errorf("打开数据存储: %w", err)
	}
	defer applicationStore.Close()
	service := workflow.New(applicationStore, analyzer.New())
	server := &http.Server{Addr: addr, Handler: httpui.New(service).Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 20 * time.Second, IdleTimeout: 60 * time.Second}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s: %w", addr, err)
	}
	errCh := make(chan error, 1)
	go func() { errCh <- server.Serve(listener) }()
	log.Printf("舞台吊挂演出放行台监听 http://%s", listener.Addr())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	select {
	case sig := <-signals:
		log.Printf("收到 %s，开始关闭", sig)
	case serveErr := <-errCh:
		if !errors.Is(serveErr, http.ErrServerClosed) {
			return serveErr
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err = server.Shutdown(ctx); err != nil {
		return fmt.Errorf("优雅关闭: %w", err)
	}
	serveErr := <-errCh
	if !errors.Is(serveErr, http.ErrServerClosed) {
		return serveErr
	}
	return nil
}

func resolveAddress(explicit, portEnv string) (string, error) {
	addr := explicit
	if addr == "" {
		if portEnv != "" {
			port, err := strconv.Atoi(portEnv)
			if err != nil || port < 1 || port > 65535 {
				return "", fmt.Errorf("PORT 必须是 1 到 65535 的端口号")
			}
			addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		} else {
			addr = "127.0.0.1:19081"
		}
	}
	host, portText, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("监听地址必须为 host:port: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return "", fmt.Errorf("监听端口必须是 1 到 65535")
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		return "", fmt.Errorf("监听地址必须使用 127.0.0.1、::1 或 localhost 回环地址")
	}
	return addr, nil
}

func runSelfcheck(addr string) error {
	tempDir, err := os.MkdirTemp("", "stageclearance-selfcheck-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tempDir)
	applicationStore, err := store.Open(filepath.Join(tempDir, "selfcheck.db"))
	if err != nil {
		return err
	}
	defer applicationStore.Close()
	service := workflow.New(applicationStore, analyzer.New())
	server := &http.Server{Handler: httpui.New(service).Handler(), ReadHeaderTimeout: 3 * time.Second}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("自检监听 %s: %w", addr, err)
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	baseURL := "http://" + listener.Addr().String()
	flowErr := executeSelfcheck(ctx, baseURL)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer shutdownCancel()
	shutdownErr := server.Shutdown(shutdownCtx)
	serverErr := <-serveErr
	if flowErr != nil {
		return fmt.Errorf("HTTP 主流程自检失败: %w", flowErr)
	}
	if shutdownErr != nil {
		return shutdownErr
	}
	if !errors.Is(serverErr, http.ErrServerClosed) {
		return serverErr
	}
	fmt.Printf("自检通过：已在 %s 经 HTTP 完成建档、建模、分析、排练、独立复核、冻结、放行与凭据校验\n", listener.Addr())
	return nil
}
