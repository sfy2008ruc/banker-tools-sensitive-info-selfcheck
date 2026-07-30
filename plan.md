# 技术方案 plan.md — 客户经理电脑敏感文件自查工具 V1

> 依据：[spec-version1.md](spec-version1.md)。本文回答"怎么实现"，spec 回答"做什么"。

## 1. 总体技术决策

| 决策点 | 结论 | 理由 |
|--------|------|------|
| 语言/运行时 | Go（本机 1.26），**仅用标准库，零第三方依赖** | 内网环境无法 `go get`；标准库足以覆盖 HTTP、遍历、embed、JSON、模板；零依赖也最大限度降低杀软误报面 |
| 前端 | **原生 HTML + CSS + JS 单文件**，不用 Vue/Vite | 免去 Node 构建链；页面只有 3 个视图，原生足够；天然满足 Chrome 90 基线（只用 ES2017 以内语法，不用 `:has()`） |
| 前后端通信 | REST + 500ms 轮询进度 | 避免 WebSocket 复杂度（spec 4.3） |
| 回收站 | Windows：shell32 `SHFileOperationW`（syscall 直调，不引入 x/sys）；Linux：FreeDesktop Trash 规范手写；macOS（仅开发用）：移入 `~/.Trash` | 保持零依赖 |
| 开发/测试环境 | macOS 上开发，platform 层为 darwin 提供开发用实现；交叉编译产出 windows/linux 产物 | darwin 分支只服务于本机开发验证，不随包分发 |

## 2. 代码结构

```
manager-self-check/
├── go.mod
├── cmd/selfcheck/main.go        # 入口：端口/token/浏览器/优雅退出
├── internal/
│   ├── rules/       rules.go rules_test.go      # 规则加载、默认规则、路径展开前的原始规则
│   ├── platform/    platform.go trash_*.go      # OS 差异：路径展开、扫描根、回收站、开浏览器
│   ├── scanner/     scanner.go scanner_test.go  # 并发遍历、命中逻辑、进度、取消
│   ├── quarantine/  quarantine.go *_test.go     # 暂存区、manifest、还原、删除
│   ├── report/      report.go                   # HTML 自查报告
│   └── server/      server.go server_test.go    # HTTP API、token/Origin 校验、心跳
├── web/dist/index.html          # 前端单文件（go:embed）
├── rules.json                   # 外置默认规则（与内置一致）
├── build/build.sh               # 三平台交叉编译 + 打包
└── spec-version1.md / plan.md / tasks.md
```

依赖方向：`main → server → scanner/quarantine/report → rules/platform`。internal 各包互不横向依赖（scanner、quarantine 均只依赖 rules/platform）。

## 3. 各模块设计

### 3.1 rules

- `Rules` 结构体字段与 spec 5.1 的 JSON 一一对应（keywords / extensions / hotDirs / scanRoots / excludeDirs / minImageSizeKB / maxFileSizeMB）。
- `Load(exeDir)`：读取程序同目录 `rules.json`；不存在或解析失败 → 返回内置默认规则并记录 fallback 原因（页面展示规则版本时附注"内置规则"）。
- `ForOS(goos)`：把 map 里 windows/linux 两组取出为扁平的 `Resolved` 结构（darwin 复用 linux 组），路径展开交给 platform。

### 3.2 platform

- `Expand(path)`：Windows 展开 `%VAR%`（正则 + `os.Getenv`），Unix 展开前缀 `~`。
- `OSKey()`：`windows` / 其余一律 `linux`（含 darwin 开发机）。
- `MoveToTrash(path)`：三个 build-tag 文件：
  - `trash_windows.go`：`SHFileOperationW`，`FO_DELETE|FOF_ALLOWUNDO|FOF_NOCONFIRMATION|FOF_SILENT`，路径双 NUL 结尾 UTF-16；
  - `trash_linux.go`：移入 `~/.local/share/Trash/files/`，同步写 `info/*.trashinfo`（含原路径与时间），重名加序号；
  - `trash_darwin.go`：移入 `~/.Trash`（重名加时间戳）。
- `OpenBrowser(url)`：windows `rundll32 url.dll,FileProtocolHandler` → 失败退 `cmd /c start`；linux `xdg-open`；darwin `open`。失败不致命，控制台/日志打印地址（spec 风险表）。

### 3.3 scanner

- `Scan(ctx, resolved, opts)`：每个扫描根一个 goroutine + `filepath.WalkDir`；共享 `Progress`（互斥锁保护：DirsScanned / Found / SkippedNoAccess / Status）。
- 命中判定（spec 5.2）：
  1. 文件名含任一敏感词 → `keyword:<词>`；
  2. 后缀命中 且 位于热点目录树内 → `ext:<后缀>@hotdir`；
  3. 双命中 → `risk=high`（前端预勾选依据）；仅后缀 → `risk=normal`。
- 过滤：排除目录（前缀匹配，含暂存区、程序自身目录）；图片 < minImageSizeKB 跳过；> maxFileSizeMB 打 `大文件` 标签但仍列出。
- 去重：多根重叠时按绝对路径 map 去重。
- 取消：`context.Cancel`，WalkDir 回调里检查。

### 3.4 quarantine

- 暂存区：`<home>/待删除文件区/`，批次目录 `20060102-150405`，文件 `%04d_原名`。
- `manifest.json`：spec 6.2 字段，**每次变更立即原子落盘**（写临时文件 + rename），保证强杀进程后状态一致（spec 风险表、验收 3）。
- `MoveIn(files)`：逐文件 moveFile；单文件失败不影响其余，逐条返回结果。
- `moveFile`：`os.Rename` → 跨盘错误时 copy + 校验大小 + 删源；任何失败保留源文件（spec 6.3）。
- `Restore(ids)`：移回 originalPath；已存在 → `原名(还原-150405).后缀`；目录不存在则 MkdirAll。
- `Delete(ids)`：`platform.MoveToTrash(存储路径)` → status=deleted。
- 安全边界：Delete/Restore 只接受 manifest 中的 id；MoveIn 只接受本次扫描结果中的路径（server 层校验，spec 9.4）。

### 3.5 server

- `http.ServeMux`；中间件链：仅回环来源 → `X-Token` 校验（`crypto/subtle` 比较）→ Origin/Referer 校验（POST 时必须同源）。
- 静态页免 token（token 经 URL `?token=` 交给前端 JS 存内存、放请求头）。
- 路由：spec 第 7 章 11 个接口原样实现；`/api/scan` 进行中返回 409。
- 心跳：`/api/heartbeat` 刷新时间戳；watchdog 每 30s 检查，超 10 分钟无心跳 → 优雅 Shutdown。`/api/exit` 同路径退出。
- 状态持有：`Server{rules, scanner 当前实例, lastResults map[path]Result, quarantine}`。

### 3.6 report

- `html/template` 内置模板：自查时间、机器名/用户名、规则版本、扫描范围、发现/暂存/还原/删除计数、明细表（来自 manifest 终态 + 本次扫描统计）。
- 落盘 `待删除文件区/reports/自查报告-20060102-150405.html`，同时作为响应返回。

### 3.7 前端（web/dist/index.html）

- 三视图切换（扫描页 / 结果页 / 暂存区页），状态放单个 JS 对象；`fetch` 封装统一带 `X-Token`。
- 结果页：三组分区渲染（高风险预勾选 / 敏感词命中 / 热点后缀命中），排序（修改时间、大小）、后缀筛选、全选反选、路径点击复制。
- 心跳 `setInterval 30s`；"完成并退出"→ `/api/exit` 后展示结束页。
- 兼容约束：不用可选链之后于 ES2017 的语法糖（Chrome 90 实际支持 ES2020，留安全余量）；不用 `:has()`；全部内联，零外部资源。

## 4. 实施顺序与验证策略

按 [tasks.md](tasks.md) 的 T01→T12 顺序，**每个任务完成即运行其验收命令**（`go build` / `go test ./...` / 本机端到端运行）。

- 单元测试：rules（加载/fallback）、scanner（临时目录造样本文件）、quarantine（移动/冲突/还原/强杀恢复模拟）、server（httptest：401/409/正常流）。
- 端到端（macOS 开发机）：造样本文件 → 启动 → 浏览器全流程 → 检查暂存区、回收站、报告产物。
- 交叉编译验证：`GOOS=windows/linux × GOARCH=amd64/arm64` 全部编译通过（Windows/信创实机验证属 spec M4，超出本次开发环境能力，在 tasks 中标注为"待实机"）。

## 5. 本次开发范围声明

- 交付 T01–T11 全部代码与三平台构建产物，macOS 上可端到端演示；
- spec M0（管控软件白名单）、M4（信创实机）、M5（试点）为组织协调事项，不在代码交付范围内；
- Windows 回收站 syscall 与 Linux Trash 代码按规范实现并编译通过，但**实际行为需在对应实机上验证**（tasks.md 中明确标注）。
