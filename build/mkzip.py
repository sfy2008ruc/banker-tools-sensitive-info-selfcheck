#!/usr/bin/env python3
"""打包 zip，确保中文文件名带 UTF-8 标志（general purpose bit 11）。

macOS 自带的 zip 命令不设置该标志，Windows 资源管理器会按 GBK 解码，
中文文件名显示成乱码，用户根本看不出该双击哪个文件（V1.2.1 用户反馈）。
Python 的 zipfile 对非 ASCII 文件名会自动置位，同时这里保留可执行权限。
"""
import os
import sys
import zipfile


def main(src_dir: str, out_zip: str) -> None:
    with zipfile.ZipFile(out_zip, "w", zipfile.ZIP_DEFLATED) as z:
        for root, dirs, files in os.walk(src_dir):
            dirs.sort()
            for name in sorted(files):
                full = os.path.join(root, name)
                rel = os.path.relpath(full, src_dir)
                info = zipfile.ZipInfo.from_file(full, rel)
                info.compress_type = zipfile.ZIP_DEFLATED
                # 保留权限位（.app 内的可执行文件、start.sh 等需要 +x）
                mode = os.stat(full).st_mode
                info.external_attr = (mode & 0xFFFF) << 16
                with open(full, "rb") as f:
                    z.writestr(info, f.read())

    with zipfile.ZipFile(out_zip) as z:
        bad = [i.filename for i in z.infolist()
               if not i.filename.isascii() and not (i.flag_bits & 0x800)]
        if bad:
            sys.exit(f"错误：以下条目缺少 UTF-8 标志，Windows 上会乱码：{bad}")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("用法: mkzip.py <源目录> <输出 zip>")
    main(sys.argv[1], sys.argv[2])
