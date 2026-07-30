#!/usr/bin/env bash
# 三平台交叉编译与打包（spec 第 10 章）。产物输出到 build/out/。
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="v1.1.0"
OUT="build/out"
rm -rf "$OUT" && mkdir -p "$OUT"

LDFLAGS="-s -w"

stage() { # stage <目录名> <可执行文件路径>
  local dir="$OUT/$1"
  mkdir -p "$dir"
  cp rules.json "$dir/"
  cat > "$dir/使用说明.txt" <<'EOF'
《客户经理电脑敏感文件自查工具》使用说明

一、如何选择安装包
  - Windows 10 电脑            → 自查工具-*-windows-x64.zip
  - 苹果 Mac 电脑              → 自查工具-*-macos-universal.zip（Intel/M 芯片通用）
  - 信创/Linux（海光/兆芯）    → 自查工具-*-信创-x64.tar.gz
  - 信创/Linux（飞腾/鲲鹏）    → 自查工具-*-信创-arm64.tar.gz
  不确定机型时可先试 x64 包，程序会在不匹配时给出提示。

二、使用步骤（无需安装、无需管理员权限）
  1. 将压缩包解压到任意位置（如桌面）
  2. 启动：
     - Windows：双击 selfcheck.exe
     - Mac：双击"启动自查工具.command"（首次如提示无法验证开发者，
       请在文件上右键→打开，或到"系统设置→隐私与安全性"点"仍要打开"）
     - 信创/Linux：双击 start.sh（或"启动自查工具"图标）
  3. 浏览器会自动打开自查页面，按页面提示操作：
     扫描 → 勾选可疑文件 → 移入暂存区 → 确认删除 → 导出自查报告
  4. 报告自动保存在"主目录/待删除文件区/reports/"下，请留存备查

三、说明
  - 删除的文件先进入系统回收站，误删可在回收站找回
  - 工具全程不联网、不上传任何数据
  - rules.json 为扫描规则，由合规部门统一更新，请勿自行修改
EOF
}

echo "== windows/amd64"
GOOS=windows GOARCH=amd64 go build -ldflags "$LDFLAGS -H windowsgui" -o "$OUT/win/selfcheck.exe" ./cmd/selfcheck
stage win "selfcheck.exe"
(cd "$OUT/win" && zip -qr "../自查工具-$VERSION-windows-x64.zip" .)

linux_pkg() { # linux_pkg <goarch> <目录名> <包名架构标识>
  local arch="$1" tag="$2" label="$3" dir="$OUT/$2"
  echo "== linux/$arch"
  GOOS=linux GOARCH="$arch" go build -ldflags "$LDFLAGS" -o "$OUT/$tag/selfcheck" ./cmd/selfcheck
  stage "$tag" selfcheck
  cat > "$dir/start.sh" <<'EOF'
#!/bin/bash
cd "$(dirname "$0")"
chmod +x ./selfcheck 2>/dev/null
./selfcheck
EOF
  cat > "$dir/启动自查工具.desktop" <<EOF
[Desktop Entry]
Type=Application
Name=电脑敏感文件自查工具
Comment=扫描并清理电脑中的客户敏感文件
Exec=bash -c 'cd "\$(dirname "%k")" && ./start.sh'
Terminal=false
Categories=Utility;
EOF
  chmod +x "$dir/selfcheck" "$dir/start.sh"
  (cd "$OUT/$tag" && tar -czf "../自查工具-$VERSION-信创-$label.tar.gz" .)
}

linux_pkg amd64 linux-x64 x64
linux_pkg arm64 linux-arm64 arm64

echo "== macOS universal (amd64 + arm64)"
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$OUT/mac/selfcheck-amd64" ./cmd/selfcheck
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$OUT/mac/selfcheck-arm64" ./cmd/selfcheck
if command -v lipo >/dev/null; then
  lipo -create -output "$OUT/mac/selfcheck" "$OUT/mac/selfcheck-amd64" "$OUT/mac/selfcheck-arm64"
  rm "$OUT/mac/selfcheck-amd64" "$OUT/mac/selfcheck-arm64"
else
  mv "$OUT/mac/selfcheck-arm64" "$OUT/mac/selfcheck" && rm -f "$OUT/mac/selfcheck-amd64"
fi
stage mac selfcheck
cat > "$OUT/mac/启动自查工具.command" <<'EOF'
#!/bin/bash
cd "$(dirname "$0")"
# 去掉浏览器下载/邮件附件解压带上的隔离标记，避免 Gatekeeper 拦截
xattr -dr com.apple.quarantine ./selfcheck 2>/dev/null
chmod +x ./selfcheck
./selfcheck
EOF
chmod +x "$OUT/mac/selfcheck" "$OUT/mac/启动自查工具.command"
(cd "$OUT/mac" && zip -qry "../自查工具-$VERSION-macos-universal.zip" .)

echo "== 生成 GitHub 发布用 ASCII 命名副本（GitHub 会剥离附件名中的中文）"
(cd "$OUT" &&
  cp "自查工具-$VERSION-windows-x64.zip"     "selfcheck-$VERSION-windows-x64.zip" &&
  cp "自查工具-$VERSION-macos-universal.zip" "selfcheck-$VERSION-macos-universal.zip" &&
  cp "自查工具-$VERSION-信创-x64.tar.gz"     "selfcheck-$VERSION-xinchuang-linux-x64.tar.gz" &&
  cp "自查工具-$VERSION-信创-arm64.tar.gz"   "selfcheck-$VERSION-xinchuang-linux-arm64.tar.gz")

echo "== 生成校验和"
(cd "$OUT" && shasum -a 256 selfcheck-$VERSION-* > SHA256SUMS.txt)

echo
echo "== 产物清单"
ls -lh "$OUT" | grep -E 'zip|tar|SHA'
