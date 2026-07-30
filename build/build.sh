#!/usr/bin/env bash
# 三平台交叉编译与打包（spec 第 10 章）。产物输出到 build/out/。
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="v1.1.2"
OUT="build/out"
rm -rf "$OUT" && mkdir -p "$OUT"

LDFLAGS="-s -w"

# Windows 图标：cmd/selfcheck/rsrc_windows_amd64.syso 已入库，构建时自动链接；
# 更换图标后用以下命令重新生成（需要联网）：
#   go run github.com/akavel/rsrc@latest -ico assets/icon.ico -arch amd64 -o cmd/selfcheck/rsrc_windows_amd64.syso

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
     - Mac：双击"敏感自查工具.app"（首次如提示无法验证开发者，
       请在图标上右键→打开，或到"系统设置→隐私与安全性"点"仍要打开"；
       Mac 版规则文件位于 app 内：右键→显示包内容→Contents/MacOS/rules.json）
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
  cp assets/icon-256.png "$dir/icon.png"
  cat > "$dir/启动自查工具.desktop" <<EOF
[Desktop Entry]
Type=Application
Icon=icon.png
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

echo "== macOS universal (amd64 + arm64) → .app 包"
GOOS=darwin GOARCH=amd64 go build -ldflags "$LDFLAGS" -o "$OUT/mac-bin/selfcheck-amd64" ./cmd/selfcheck
GOOS=darwin GOARCH=arm64 go build -ldflags "$LDFLAGS" -o "$OUT/mac-bin/selfcheck-arm64" ./cmd/selfcheck
APP="$OUT/mac/敏感自查工具.app"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
if command -v lipo >/dev/null; then
  lipo -create -output "$APP/Contents/MacOS/selfcheck" "$OUT/mac-bin/selfcheck-amd64" "$OUT/mac-bin/selfcheck-arm64"
else
  cp "$OUT/mac-bin/selfcheck-arm64" "$APP/Contents/MacOS/selfcheck"
fi
rm -rf "$OUT/mac-bin"
cp assets/icon.icns "$APP/Contents/Resources/icon.icns"
cp rules.json "$APP/Contents/MacOS/rules.json"
cat > "$APP/Contents/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleName</key><string>敏感自查工具</string>
  <key>CFBundleDisplayName</key><string>敏感自查工具</string>
  <key>CFBundleExecutable</key><string>selfcheck</string>
  <key>CFBundleIdentifier</key><string>io.github.sfy2008ruc.selfcheck</string>
  <key>CFBundleVersion</key><string>${VERSION#v}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION#v}</string>
  <key>CFBundleIconFile</key><string>icon</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>NSHighResolutionCapable</key><true/>
  <key>LSMinimumSystemVersion</key><string>10.13</string>
</dict></plist>
EOF
chmod +x "$APP/Contents/MacOS/selfcheck"
stage mac selfcheck
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
