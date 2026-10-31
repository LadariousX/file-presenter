package library

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Favorite files are moved into this folder at the top of the presented folder.
const favoritesDir = "Favorites"

// ToggleFlag moves the current file into <root>/Favorites, or back to where it came from.
func (lib *Library) ToggleFlag() error {
	lib.mu.Lock()
	defer lib.mu.Unlock()
	i := lib.index
	favDir := filepath.Join(lib.root, favoritesDir)
	records := map[string]string{} // name in Favorites -> original rel path
	recordsFile := filepath.Join(favDir, ".favorites.json")
	if data, err := os.ReadFile(recordsFile); err == nil {
		json.Unmarshal(data, &records)
	}

	var dest string
	name := filepath.Base(lib.files[i])
	if lib.dirs[i] == favoritesDir {
		orig, ok := records[name]
		if !ok {
			return fmt.Errorf("don't know where %s came from", name)
		}
		dest = filepath.Join(lib.root, filepath.FromSlash(orig))
		if _, err := os.Stat(dest); err == nil {
			return fmt.Errorf("%s already exists", orig)
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		delete(records, name)
	} else {
		if err := os.MkdirAll(favDir, 0o755); err != nil {
			return err
		}
		ext := filepath.Ext(name)
		dest = filepath.Join(favDir, name)
		for k := 2; ; k++ { // don't overwrite a same-named favorite
			if _, err := os.Stat(dest); err != nil {
				break
			}
			dest = filepath.Join(favDir, fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), k, ext))
		}
		records[filepath.Base(dest)] = lib.rels[i]
	}

	if err := os.Rename(lib.files[i], dest); err != nil {
		return err
	}
	if len(records) == 0 {
		os.Remove(recordsFile)
		os.Remove(favDir) // only succeeds if nothing else is in there
	} else {
		b, _ := json.MarshalIndent(records, "", "  ")
		os.WriteFile(recordsFile, b, 0o644)
	}

	lib.files[i] = dest
	lib.rels[i], lib.dirs[i] = lib.relDir(dest)
	lib.saveProgress()
	lib.changed()
	return nil
}
