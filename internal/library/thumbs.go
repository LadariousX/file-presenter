package library

import (
	"bytes"
	"container/list"
	"fmt"
	"image"
	_ "image/png"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/disintegration/imaging"
)

// Thumbnails are JPEGs kept only in memory (least recently used dropped first).
const (
	thumbSize    = 400
	thumbQuality = 80
	thumbLimit   = 600 // ~25 KB each
)

var thumbs = struct {
	sync.Mutex
	order *list.List // front = most recent; values are thumbKey
	items map[thumbKey]*list.Element
	data  map[thumbKey][]byte
}{order: list.New(), items: map[thumbKey]*list.Element{}, data: map[thumbKey][]byte{}}

type thumbKey struct {
	path string
	kind string
}

var thumbSlots = make(chan struct{}, 4) // limit concurrent decodes / ffmpeg / qlmanage

// Thumb returns a small JPEG preview of path (of the given kind), made once and kept in memory.
func Thumb(path, kind string) ([]byte, error) {
	key := thumbKey{path, kind}
	thumbs.Lock()
	if el, ok := thumbs.items[key]; ok {
		thumbs.order.MoveToFront(el)
		b := thumbs.data[key]
		thumbs.Unlock()
		return b, nil
	}
	thumbs.Unlock()

	thumbSlots <- struct{}{}
	img := thumbSource(path, kind)
	<-thumbSlots
	if img == nil {
		return nil, fmt.Errorf("no thumbnail for %s", filepath.Base(path))
	}
	var buf bytes.Buffer
	if err := imaging.Encode(&buf, imaging.Fit(img, thumbSize, thumbSize, imaging.Linear), imaging.JPEG,
		imaging.JPEGQuality(thumbQuality)); err != nil {
		return nil, err
	}

	thumbs.Lock()
	defer thumbs.Unlock()
	if _, ok := thumbs.items[key]; !ok {
		thumbs.items[key] = thumbs.order.PushFront(key)
		thumbs.data[key] = buf.Bytes()
		for thumbs.order.Len() > thumbLimit {
			old := thumbs.order.Remove(thumbs.order.Back()).(thumbKey)
			delete(thumbs.items, old)
			delete(thumbs.data, old)
		}
	}
	return buf.Bytes(), nil
}

func thumbSource(path, kind string) image.Image {
	switch kind {
	case "image":
		if img, err := imaging.Open(path, imaging.AutoOrientation(true)); err == nil {
			return img
		}
	case "video":
		for _, seek := range [][]string{{"-ss", "1"}, {}} { // retry from 0 for clips shorter than 1s
			args := append(append([]string{"-v", "error"}, seek...), "-i", path, "-frames:v", "1",
				"-vf", fmt.Sprintf("scale=%d:-2", thumbSize), "-f", "image2pipe", "-vcodec", "png", "-")
			c, cancel := command(10*time.Second, Tool("ffmpeg"), args...)
			png, err := c.Output()
			cancel()
			if err == nil && len(png) > 0 {
				if img, _, err := image.Decode(bytes.NewReader(png)); err == nil {
					return img
				}
			}
		}
	}
	// Quick Look (Mac only) covers PDFs, docs, HEIC, videos without ffmpeg and most everything else;
	// elsewhere the caller falls back to a placeholder showing the file extension.
	if runtime.GOOS != "darwin" {
		return nil
	}
	dir, err := os.MkdirTemp("", "file-presenter-ql")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	c, cancel := command(8*time.Second, "qlmanage", "-t", "-s", fmt.Sprint(thumbSize), "-o", dir, path)
	c.Run()
	cancel()
	img, err := imaging.Open(filepath.Join(dir, filepath.Base(path)+".png"))
	if err != nil {
		return nil
	}
	return img
}
