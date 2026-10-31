package main

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"image"
	_ "image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/disintegration/imaging"
)

var cacheDir = ".cache"

// cachePath names a cache file by the source's path, size and mtime so edits invalidate it.
func cachePath(path, suffix string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d", path, st.Size(), st.ModTime().UnixNano())))
	return filepath.Join(cacheDir, fmt.Sprintf("%x%s", key, suffix)), nil
}

// cached returns out if it exists, else runs make(tmp) and renames tmp into place.
func cached(path, suffix string, make func(tmp string) error) (string, error) {
	out, err := cachePath(path, suffix)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(out); err == nil {
		return out, nil
	}
	tmp := fmt.Sprintf("%s.%d.part%s", strings.TrimSuffix(out, suffix), time.Now().UnixNano(), suffix)
	if err := make(tmp); err != nil {
		os.Remove(tmp)
		return "", err
	}
	return out, os.Rename(tmp, out)
}

func makeThumb(path, kind string) (string, error) {
	return cached(path, ".thumb.jpg", func(tmp string) error {
		img := thumbSource(path, kind)
		if img == nil {
			return fmt.Errorf("no thumbnail for %s", filepath.Base(path))
		}
		return imaging.Save(imaging.Fit(img, 400, 400, imaging.Linear), tmp, imaging.JPEGQuality(80))
	})
}

func thumbSource(path, kind string) image.Image {
	switch kind {
	case "image", "image_conv":
		if img, err := imaging.Open(path, imaging.AutoOrientation(true)); err == nil {
			return img
		}
	case "video", "video_conv":
		for _, seek := range [][]string{{"-ss", "1"}, {}} { // retry from 0 for clips shorter than 1s
			args := append(append([]string{"-v", "error"}, seek...), "-i", path, "-frames:v", "1",
				"-vf", "scale=400:-2", "-f", "image2pipe", "-vcodec", "png", "-")
			if png, err := exec.Command("ffmpeg", args...).Output(); err == nil && len(png) > 0 {
				if img, _, err := image.Decode(bytes.NewReader(png)); err == nil {
					return img
				}
			}
		}
		return nil
	}
	// Quick Look covers PDFs, docs, HEIC and most everything else.
	dir, err := os.MkdirTemp(cacheDir, "ql")
	if err != nil {
		return nil
	}
	defer os.RemoveAll(dir)
	exec.Command("qlmanage", "-t", "-s", "400", "-o", dir, path).Run()
	img, err := imaging.Open(filepath.Join(dir, filepath.Base(path)+".png"))
	if err != nil {
		return nil
	}
	return img
}

// convertImage turns TIF/HEIC into a JPEG the browser can show.
func convertImage(path string) (string, error) {
	return cached(path, ".jpg", func(tmp string) error {
		return exec.Command("sips", "-s", "format", "jpeg", path, "--out", tmp).Run()
	})
}

// convertDoc turns DOCX/RTF/etc. into HTML.
func convertDoc(path string) (string, error) {
	return cached(path, ".html", func(tmp string) error {
		html, err := exec.Command("textutil", "-convert", "html", "-stdout", path).Output()
		if err != nil || len(html) == 0 {
			html = []byte("<p>Could not convert document.</p>")
		}
		return os.WriteFile(tmp, html, 0o644)
	})
}

// convertVideo remuxes (h264) or transcodes a video to a browser-playable MP4.
func convertVideo(path string) (string, error) {
	return cached(path, ".mp4", func(tmp string) error {
		out, _ := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries",
			"stream=codec_name", "-of", "csv=p=0", path).Output()
		codec := strings.Trim(string(out), " \n,")
		vcodec := []string{"-c:v", "copy"}
		if codec != "h264" {
			vcodec = []string{"-c:v", "libx264", "-preset", "veryfast", "-crf", "22", "-pix_fmt", "yuv420p"}
		}
		log.Printf("converting %s (%s) ...", filepath.Base(path), codec)
		args := append(append([]string{"-v", "error", "-y", "-i", path}, vcodec...),
			"-c:a", "aac", "-movflags", "+faststart", tmp)
		var stderr bytes.Buffer
		cmd := exec.Command("ffmpeg", args...)
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := stderr.String()
			if len(msg) > 500 {
				msg = msg[len(msg)-500:]
			}
			return fmt.Errorf("%v: %s", err, msg)
		}
		log.Printf("converted %s", filepath.Base(path))
		return nil
	})
}
