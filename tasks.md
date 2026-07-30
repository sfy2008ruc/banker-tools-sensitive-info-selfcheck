# tasks.md — V1 原子任务列表（含可验证标准）

> 依据 [spec-version1.md](spec-version1.md) 与 [plan.md](plan.md)。按顺序执行，每个任务完成必须通过其"验收标准"后才进入下一个。
> 状态标记：`[ ]` 未开始 / `[x]` 完成 / `[~]` 完成但需实机复验。

## T01 项目骨架与构建通路
- [x] `go mod init`，建立 plan.md 第 2 章的目录结构，`main.go` 输出版本号占位
- **验收**：`go build ./...` 成功；`go vet ./...` 无告警

## T02 rules 模块（spec 5.1）
- [x] `Rules` 结构体 + 内置默认规则 + `Load()`（同目录 rules.json，缺失/损坏 fallback 内置）+ `ForOS()`
- [x] 提交外置 `rules.json`（内容与内置一致）
- **验收**：`go test ./internal/rules/` 通过，用例覆盖：正常加载、文件缺失 fallback、JSON 损坏 fallback、darwin 映射到 linux 组

## T03 platform 模块
- [x] `Expand()`（%VAR% / ~）、`OSKey()`、`OpenBrowser()`、三个 build-tag 的 `MoveToTrash()`
- **验收**：`go test ./internal/platform/` 通过（Expand 各平台用例）；`GOOS=windows go build ./...` 与 `GOOS=linux go build ./...` 均编译通过（证明 build-tag 文件语法正确）

## T04 scanner 扫描引擎（spec 5.2）
- [x] 并发 WalkDir、命中判定（敏感词 / 后缀@热点目录 / 双命中=high）、图片最小体积过滤、大文件标签、排除目录、去重、进度、取消
- **验收**：`go test ./internal/scanner/` 通过，用例在临时目录构造样本并断言：敏感词命中、热点后缀命中、双命中 risk=high、非热点目录后缀不命中、小图片被过滤、排除目录被跳过、取消后 status=cancelled

## T05 quarantine 暂存区与 manifest（spec 6）
- [x] 批次目录 + 序号前缀、manifest 原子落盘、MoveIn 逐条结果、moveFile（rename→copy 校验→删源，失败保源）、Restore（冲突改名、目录重建）
- **验收**：`go test ./internal/quarantine/` 通过，用例覆盖：移入后 manifest 状态 pending 且源文件消失、还原到原路径、还原冲突时改名、目标目录被删后还原自动重建、重新 Load manifest 状态一致（模拟强杀重启）

## T06 回收站删除（spec 6.3）
- [x] `Delete(ids)`：调 `platform.MoveToTrash`，状态置 deleted
- **验收**：`go test ./internal/quarantine/` 删除用例通过（darwin 上验证文件从暂存区消失且进入 `~/.Trash`）；Windows/Linux 路径 `[~]` 待实机复验

## T07 report 报告生成（spec 第 7 章 /api/report）
- [x] HTML 模板：自查时间、用户/主机、规则版本、扫描统计、处理明细；落盘 `reports/`
- **验收**：单测断言生成的 HTML 包含关键字段；文件成功写入 reports/ 目录

## T08 server HTTP API（spec 第 7、9 章）
- [x] 11 个路由、X-Token 中间件（subtle 比较）、Origin 校验、409 扫描互斥、路径边界校验（MoveIn 仅接受扫描结果内路径、Delete/Restore 仅接受 manifest id）、心跳 watchdog、/api/exit
- **验收**：`go test ./internal/server/` 通过（httptest）：无 token→401、错 token→401、扫描中再扫→409、伪造路径 quarantine→400、正常链路 scan→results→quarantine→restore→delete→report 全通

## T09 前端页面（spec 第 8 章）
- [~] `web/dist/index.html` 单文件三视图：扫描页（进度/取消）、结果页（三分组、预勾选、排序、筛选、复制路径）、暂存区页（还原/全部还原/二次确认删除/导出报告/完成退出）；心跳；中文大字号
- **验收**：`go build`（embed 生效）；本机启动后浏览器手工走查三页功能可用；页面零外部资源请求（DevTools Network 确认）

## T10 主程序集成与退出机制
- [x] main：随机端口 + 随机 token、自动开浏览器（失败打印地址）、优雅退出（exit 接口 / 心跳超时 / Ctrl-C）
- **验收**：macOS 端到端演示：造样本文件 → 双击/命令行启动 → 全流程（扫描→勾选→暂存→还原→删除→报告）→ 退出；强杀进程重启后暂存区状态一致（对应 spec 验收 1/2/3）

## T11 三平台交叉编译与打包（spec 第 10 章）
- [x] `build/build.sh`：windows/amd64（`-H windowsgui`）、linux/amd64、linux/arm64，产出 3 个压缩包（含可执行文件、rules.json、使用说明.txt、Linux 附 start.sh 与 .desktop）
- **验收**：脚本一次跑通产出 3 个包；`file` 命令确认各产物架构正确；zip/tar 内容清单符合 spec 10.1

## T12 端到端验收（spec 第 13 章，可在开发机完成的部分）
- [x] 样本集验证：≥20 个样本（敏感词命中/热点后缀/双命中/同名冲突/大文件/小图片过滤），全部按规则正确处理、无文件丢失
- [x] `curl` 验证：错误 token 401、非回环拒绝（代码走查）、端口仅监听 127.0.0.1（`lsof` 确认）
- [x] 全程无出站请求（代码零外部 URL + 页面零外部资源）
- **验收**：以上三条全部通过并在本文件勾选；Windows/信创实机项标注 `[~]` 移交 spec M4

## 实机复验清单（本次开发环境无法覆盖，移交 M4）
- Windows：回收站删除行为、`-H windowsgui` 无黑框、浏览器调起、%VAR% 展开、D/E/F 盘扫描
- 信创 x64 / ARM64：Trash 规范落位、xdg-open 调起自带浏览器、中文目录、.desktop 双击启动

---

# V1.1 增量任务（2026-07-30 用户确认的四项改进）

## T13 补充搜索词（仅本次扫描生效）
- [x] `/api/scan` 接收 `extraKeywords`，与规则词合并后扫描；清洗（去空白/去重/剔除既有词/限 50 条）；不写回 Resolved、不落盘
- [x] 扫描页输入框（顿号/逗号/空白分隔），提示"仅本次生效、不能减少统一规则"
- **验收**：单测 `TestScanExtraKeywords` 通过（命中/不污染规则/仅单次生效）；沙箱实测"尽调"补充词命中后，下次不带词扫描不再命中 ✓

## T14 结果页时间筛选
- [x] 下拉筛选：全部 / 最近一个月有变动 / 一个月之前 / 三个月之前，纯前端过滤，扫描仍全量
- **验收**：页面走查筛选生效（待浏览器面板恢复后复查）`[~]`

## T15 打开文件 / 打开所在文件夹
- [x] `/api/open`（file/folder 两种模式），路径仅限本次扫描结果；platform 三平台实现（Windows rundll32/explorer /select、macOS open/-R、Linux xdg-open）
- [x] 结果行"打开/文件夹"按钮；打开文件后提示"查看后请关闭再移入"（Office 文件锁）
- **验收**：单测 `TestOpenEndpoint` 通过（伪造路径 400、注入桩确认调用）；实际弹出行为 `[~]` 待实机复验

## T16 图片缩略图（点击才显示）
- [x] `/api/preview`：标准库解码 jpg/png/gif + 邻近采样缩放至 480px、JPEG 输出、Cache-Control: no-store、50MB 上限；bmp 不支持预览
- [x] 结果行"预览"按钮，点击行下展开/收起，blob 方式携带 token 加载（img 标签无法带请求头）
- **验收**：单测 `TestPreviewEndpoint` 通过；沙箱实测返回合法 JPEG 240x160、伪造路径 400 ✓

> V1.1 版本号 v1.1.0，三平台包已重新构建。

---

# V1.2 增量任务（2026-07-30）

## T17 扫描页改版
- [x] `/api/config` 接口下发规则（敏感词、后缀、扫描范围），token 校验同其他接口
- [x] 扫描页重排：开始自查按钮置顶 → 敏感词清单（chip 展示，含数量）→ 补充搜索词输入框
- **验收**：`TestConfigEndpoint` 通过（内容正确 + 无 token 401）；页面截图确认三段式布局 ✓

## T18 图标改版（单字"查"）
- [x] 采用 D 方案：红底白字大"查"，去掉文档/放大镜等小尺寸糊掉的元素
- [x] assets 全套重生成 + .ico/.icns/.syso 更新
- **验收**：32px 缩略实测字形清晰 ✓；exe 内嵌新图标已校验 ✓

## T19 试用反馈修复（V1.2.1）
- [x] 浏览器改为 Chrome 优先：按已知安装路径直接调起 chrome.exe（Win 还兜底 Edge，Linux 查 PATH 中 chromium 系），找不到才交系统默认程序——避免 Windows 未设默认浏览器时弹"你要如何打开"选择框
- [x] Windows GUI 模式无控制台，浏览器调起失败时用 MessageBoxW 弹窗给出地址，并写入同目录"自查页面地址.txt"（此前是完全静默，用户以为双击没反应）
- [x] Windows 可执行文件改名为"双击启动-敏感自查工具.exe"，使用说明补充三步操作指引
- [x] **打包改用 build/mkzip.py**：macOS 的 zip 命令不设 UTF-8 文件名标志，Windows 解压后中文名显示为乱码（实测 `使用说明.txt` → `Σ╜┐τö¿Φ»┤µÿÄ.txt`），这很可能是"不知道点哪个文件"的真正原因
- **验收**：`TestFindChromeReturnsExistingPathOrEmpty` 通过；本机实测 Chrome 被正确拉起 ✓；zip 内条目 UTF-8 标志全部置位 ✓；mac .app 解压后 +x 保留且可运行 ✓
