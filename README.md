# file-presenter

Browse every file on a USB drive in a single browser window, controlled from your phone.

## Run

Requires Go 1.22+ and `ffmpeg` (`brew install ffmpeg`).

```
go build -o file-presenter .
./file-presenter                       # defaults to "/Volumes/NO NAME"
./file-presenter "/Volumes/OTHER" -port 9000 -no-open -cache .cache
```

The viewer opens fullscreen in its own Chromium window, with autoplay allowed so videos and audio start on their own. It falls back to Chrome, then your default browser. Press ⌃⌘F to leave fullscreen. The ← / → keys also work on the Mac.

## Remote

On your phone, open the `Remote:` URL the program prints at startup (`http://<mac-ip>:8000/remote`).

- ◀ / ▶ step through files, ⏯ plays or pauses
- **☰ List** browses folders with seen counts. Tap a file to jump to it.
- **⏭ Next unseen** skips to the next file you haven't viewed

## How files are shown

| Type | Shown as |
|---|---|
| JPG / PNG / BMP / GIF | image |
| TIF / HEIC | converted to JPEG (`sips`) |
| MP4 / MOV | video, plays directly |
| WMV / MTS / MPG / MKV | converted to MP4 with ffmpeg, next file converted ahead of time |
| MP3 | audio player |
| PDF | built-in PDF viewer |
| DOCX | converted to a web page (`textutil`) |
| anything else | thumbnail + name |

Files are ordered the way Windows Explorer shows them (folders first, natural number order), which is the order the drive's instructions assume.

Thumbnails, conversions and progress (`progress.json`) live in `.cache/`. Delete `progress.json` to reset what you've seen.
