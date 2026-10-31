package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"file-presenter/internal/library"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

// remoteCmd is a button press sent by the remote.
type remoteCmd struct {
	Type  string  `json:"type"` // go, playpause, seek, seekto, autoplay, flag, skipdupes, clear
	Delta int     `json:"delta"`
	To    *int    `json:"to"`
	At    float64 `json:"at"` // seekto: seconds
	On    bool    `json:"on"`
}

// remoteMsg is pushed to the remote: the current state, or an error to show.
type remoteMsg struct {
	Type  string         `json:"type"` // state, error
	State *library.State `json:"state,omitempty"`
	Msg   string         `json:"msg,omitempty"`
}

func (a *App) handleRemoteCmd(lib *library.Library, cmd remoteCmd) error {
	switch cmd.Type {
	case "go":
		if cmd.To != nil {
			if *cmd.To >= 0 && *cmd.To < lib.Len() {
				lib.Go(*cmd.To)
			}
		} else {
			lib.Delta(cmd.Delta)
		}
	case "playpause":
		lib.PlayPause()
	case "seek":
		lib.Seek(cmd.Delta)
	case "seekto":
		lib.SeekTo(cmd.At)
	case "flag":
		return lib.ToggleFlag()
	case "autoplay":
		lib.SetAutoplay(cmd.On)
	case "skipdupes":
		lib.SetSkipDupes(cmd.On)
	case "clear":
		lib.ClearProgress()
	}
	return nil
}

// serveRemote runs one remote's WebSocket: commands in, state pushed out whenever it changes.
func (a *App) serveRemote(w http.ResponseWriter, r *http.Request) {
	lib := a.lib.Load()
	if lib == nil {
		http.Error(w, "no folder chosen yet", http.StatusServiceUnavailable)
		return
	}
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer c.CloseNow()
	a.RemoteConnected()
	defer a.RemoteDisconnected()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	errs := make(chan string, 4)
	go func() {
		defer cancel()
		for {
			var cmd remoteCmd
			if err := wsjson.Read(ctx, c, &cmd); err != nil {
				return
			}
			if err := a.handleRemoteCmd(lib, cmd); err != nil {
				log.Printf("remote %s: %v", cmd.Type, err)
				select {
				case errs <- err.Error():
				default:
				}
			}
		}
	}()

	ping := time.NewTicker(5 * time.Second) // notice phones that vanish without closing (sleep, lost Wi-Fi)
	defer ping.Stop()
	var last []byte
	for {
		updates := lib.Updates() // grab before reading state so no change slips between
		if a.closing.Load() {
			c.Close(websocket.StatusGoingAway, "ended")
			return
		}
		s := lib.State()
		if b, _ := json.Marshal(remoteMsg{Type: "state", State: &s}); !bytes.Equal(b, last) {
			if c.Write(ctx, websocket.MessageText, b) != nil {
				return
			}
			last = b
		}
		select {
		case <-updates:
		case msg := <-errs:
			if wsjson.Write(ctx, c, remoteMsg{Type: "error", Msg: msg}) != nil {
				return
			}
		case <-ping.C:
			pctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
			err := c.Ping(pctx)
			pcancel()
			if err != nil {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}
