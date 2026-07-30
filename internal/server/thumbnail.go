package server

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"
)

// previewableExts 是可出缩略图的图片后缀（标准库可解码；bmp 无标准库解码器，不支持）。
var previewableExts = map[string]bool{".jpg": true, ".jpeg": true, ".png": true, ".gif": true}

// previewMaxSrcMB 超过此体积的图片不做预览，防止异常大图拖垮内存。
const previewMaxSrcMB = 50

// thumbnail 读取图片并缩放到最长边 maxDim，编码为 JPEG。
// 邻近采样画质对缩略图用途足够，避免引入 x/image 依赖（plan 1 零依赖原则）。
func thumbnail(path string, maxDim int) ([]byte, error) {
	if !previewableExts[strings.ToLower(filepath.Ext(path))] {
		return nil, fmt.Errorf("不支持预览的文件类型")
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > previewMaxSrcMB*1024*1024 {
		return nil, fmt.Errorf("图片过大，不生成预览")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	src, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("图片解码失败: %w", err)
	}

	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("图片尺寸异常")
	}
	scale := 1.0
	if w > h && w > maxDim {
		scale = float64(maxDim) / float64(w)
	} else if h >= w && h > maxDim {
		scale = float64(maxDim) / float64(h)
	}
	dw, dh := int(float64(w)*scale), int(float64(h)*scale)
	if dw < 1 {
		dw = 1
	}
	if dh < 1 {
		dh = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := 0; y < dh; y++ {
		sy := b.Min.Y + y*h/dh
		for x := 0; x < dw; x++ {
			sx := b.Min.X + x*w/dw
			dst.Set(x, y, src.At(sx, sy))
		}
	}

	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 75}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
