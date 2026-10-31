package library

import "time"

// Autoplay steps to the next file every autoplayDelay. Videos and audio play to the end first; a video
// the browser can't open (no length reported) moves on after the delay like a picture. It stops at the
// last file.
const autoplayDelay = 5 * time.Second

func (lib *Library) SetAutoplay(on bool) {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	lib.autoplay = on
	lib.scheduleLocked()
	lib.changed()
}

func (lib *Library) Autoplay() bool {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	return lib.autoplay
}

// scheduleLocked (re)starts the timer for the file now showing. Call with mu held.
func (lib *Library) scheduleLocked() {
	if lib.autoTimer != nil {
		lib.autoTimer.Stop()
	}
	if !lib.autoplay {
		return
	}
	v := lib.version
	lib.autoTimer = time.AfterFunc(autoplayDelay, func() {
		lib.mu.Lock()
		playing := lib.isMediaLocked() && lib.playback.dur > 0 // it'll move on when it ends instead
		ok := lib.autoplay && lib.version == v && !playing
		lib.mu.Unlock()
		if ok {
			lib.autoNext(v)
		}
	})
}

// mediaEndedLocked is called on each playback report: a finished video or song moves autoplay on.
func (lib *Library) mediaEndedLocked() {
	p := lib.playback
	if lib.autoplay && lib.isMediaLocked() && p.dur > 0 && p.paused && p.pos >= p.dur-0.5 {
		go lib.autoNext(lib.version)
	}
}

func (lib *Library) isMediaLocked() bool {
	k := lib.kinds[lib.index]
	return k == "video" || k == "audio"
}

// autoNext moves on from version v (once), or turns autoplay off at the last file.
func (lib *Library) autoNext(v int) {
	lib.mu.Lock()
	if !lib.autoplay || lib.version != v {
		lib.mu.Unlock()
		return
	}
	if lib.index == len(lib.files)-1 {
		lib.autoplay = false
		lib.changed()
		lib.mu.Unlock()
		return
	}
	lib.mu.Unlock()
	lib.Delta(1)
}
