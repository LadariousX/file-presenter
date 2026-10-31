#!/bin/sh
# Builds dist/ (run on a Mac):
#   File Presenter.app      Mac, Apple Silicon + Intel
#   file-presenter.exe      Windows, icon compiled in, no console window
#   File Presenter User Guide.pdf   from USER_GUIDE.md (needs Chrome, Chromium or Edge installed)
#
# The icon is assets/icon.png (1024×1024). After editing assets/icon.svg, redo the PNGs with Inkscape:
#   inkscape assets/icon.svg --export-type=png --export-width=880 --export-filename=icon-raw.png
#   magick icon-raw.png -background none -gravity center -extent 1024x1024 assets/icon.png && rm icon-raw.png
#   sips -z 256 256 assets/icon.png --out internal/desktop/icon.png
#   inkscape assets/icon.svg --export-text-to-path --export-plain-svg --export-filename=internal/server/web/icon.svg
set -e
cd "$(dirname "$0")"
rm -rf dist
mkdir -p dist

# Windows: go build picks up the rsrc_windows_*.syso files go-winres makes (icon + manifest).
# -H windowsgui: no console window; the app's own window is how people quit it.
go run github.com/tc-hib/go-winres@latest simply --icon assets/icon.png --manifest gui --arch amd64,arm64
GOOS=windows GOARCH=amd64 go build -ldflags=-H=windowsgui -o dist/file-presenter.exe .

# Mac: an app bundle (the window uses Cocoa, so this needs cgo and a Mac).
app="dist/File Presenter.app/Contents"
mkdir -p "$app/MacOS" "$app/Resources" dist/icon.iconset
for s in 16 32 128 256 512; do
	sips -z $s $s assets/icon.png --out dist/icon.iconset/icon_${s}x${s}.png >/dev/null
	sips -z $((s * 2)) $((s * 2)) assets/icon.png --out dist/icon.iconset/icon_${s}x${s}@2x.png >/dev/null
done
iconutil -c icns dist/icon.iconset -o "$app/Resources/icon.icns"
rm -rf dist/icon.iconset
CGO_ENABLED=1 GOARCH=arm64 CC="clang -arch arm64" go build -o dist/fp-arm64 .
CGO_ENABLED=1 GOARCH=amd64 CC="clang -arch x86_64" go build -o dist/fp-amd64 .
lipo -create -output "$app/MacOS/file-presenter" dist/fp-arm64 dist/fp-amd64
rm dist/fp-arm64 dist/fp-amd64
cat >"$app/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleName</key><string>File Presenter</string>
	<key>CFBundleDisplayName</key><string>File Presenter</string>
	<key>CFBundleIdentifier</key><string>local.file-presenter</string>
	<key>CFBundleExecutable</key><string>file-presenter</string>
	<key>CFBundleIconFile</key><string>icon</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleVersion</key><string>1.0</string>
	<key>CFBundleShortVersionString</key><string>1.0</string>
	<key>LSMinimumSystemVersion</key><string>12.0</string>
	<key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
codesign --force --deep -s - "dist/File Presenter.app"

go run ./tools/userguide USER_GUIDE.md "dist/File Presenter User Guide.pdf"

ls -l dist
