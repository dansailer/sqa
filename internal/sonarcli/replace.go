package sonarcli

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
)

// replaceFile renames src to dest, replacing dest if it already exists.
// On Unix, rename(2) already replaces; on Windows it does not.
func replaceFile(src, dest string) error {
	err := os.Rename(src, dest)
	if err == nil {
		return nil
	}
	if runtime.GOOS != "windows" {
		return err
	}
	if err2 := os.Remove(dest); err2 != nil && !errors.Is(err2, fs.ErrNotExist) {
		return err
	}
	return os.Rename(src, dest)
}
