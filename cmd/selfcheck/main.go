package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"time"

	"selfcheck/internal/platform"
	"selfcheck/internal/quarantine"
	"selfcheck/internal/rules"
	"selfcheck/internal/scanner"
	"selfcheck/internal/server"
	"selfcheck/web"
)

// Version 与规则版本一同打印在自查报告中（spec 10.2）。
const Version = "v1.2.1"

func main() {
	log.SetFlags(log.Ltime)

	exePath, err := os.Executable()
	if err != nil {
		exePath = "."
	}
	exeDir := filepath.Dir(exePath)

	home, err := os.UserHomeDir()
	if err != nil {
		fatal("无法定位用户主目录: " + err.Error())
	}

	r := rules.Load(exeDir)
	resolved := r.ForOS(runtime.GOOS)
	if resolved.BuiltinFallback {
		log.Println("提示：" + r.FallbackReason)
	}

	q, err := quarantine.Open(home)
	if err != nil {
		fatal(err.Error())
	}

	// 仅回环 + 随机端口（spec 9.1）
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal("无法监听本机端口: " + err.Error())
	}
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	static, err := fs.Sub(web.FS, "dist")
	if err != nil {
		fatal(err.Error())
	}

	srv := &server.Server{
		Version:  Version,
		Rules:    r,
		Resolved: resolved,
		Scanner:  scanner.New(),
		Quar:     q,
		Token:    token,
		ExeDir:   exeDir,
		Static:   static,
	}

	httpSrv := &http.Server{Handler: server.LoopbackOnly(srv.Handler())}
	shutdown := make(chan struct{})
	var once bool
	srv.OnExit = func() {
		if once {
			return
		}
		once = true
		close(shutdown)
	}
	// 心跳 watchdog：30s 检查一次，10 分钟无页面心跳自动退出（spec 4.1）
	srv.StartWatchdog(30*time.Second, 10*time.Minute)

	go func() {
		if err := httpSrv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Println("服务异常退出:", err)
		}
	}()

	url := fmt.Sprintf("http://%s/?token=%s", ln.Addr().String(), token)
	log.Printf("自查工具 %s 已启动（规则版本 %s）", Version, resolved.Version)
	if os.Getenv("SELFCHECK_NO_BROWSER") == "1" {
		log.Println("SELFCHECK_NO_BROWSER=1，跳过自动打开浏览器。请访问：")
	} else if err := platform.OpenBrowser(url); err != nil {
		// Windows 为 GUI 模式无控制台，必须弹窗告知，否则用户以为双击没反应
		log.Println("未能自动打开浏览器，请手动在浏览器打开以下地址：")
		platform.Notify("请手动打开浏览器",
			"未能自动打开浏览器。\n\n请复制下面的地址，粘贴到 Chrome 浏览器中打开：\n\n"+url+
				"\n\n（地址已同时保存到程序目录下的\"自查页面地址.txt\"）")
		os.WriteFile(filepath.Join(exeDir, "自查页面地址.txt"), []byte(url+"\n"), 0o644)
	} else {
		log.Println("已在浏览器中打开自查页面。若未打开，请手动访问：")
	}
	log.Println("  " + url)

	// 退出条件：页面 exit / 心跳超时 / Ctrl-C
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	select {
	case <-shutdown:
	case <-sig:
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	httpSrv.Shutdown(ctx)
	log.Println("自查工具已退出")
}

func fatal(msg string) {
	log.Println("错误：" + msg)
	// windowsgui 模式下无控制台，写一份错误说明到程序目录便于排障
	os.WriteFile("自查工具-启动失败.txt", []byte(time.Now().Format("2006-01-02 15:04:05")+"  "+msg+"\n"), 0o644)
	os.Exit(1)
}
