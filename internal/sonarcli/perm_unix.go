//go:build !windows

package sonarcli

import (
	"fmt"
	"os"
)

// PrivateFileLabel is the short description of a private credentials file
// shown by `sqa doctor`.
func PrivateFileLabel() string { return "0600" }

// PrivateDirLabel is the short description of a private directory shown by
// `sqa doctor`.
func PrivateDirLabel() string { return "0700" }

// TightenFile sets path to 0600 if it exists with any other mode, and
// returns the previous mode as a string when it changed it.
func TightenFile(path string) (changed string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Mode().Perm() == CredentialsFileMode {
		return "", nil
	}
	if err := os.Chmod(path, CredentialsFileMode); err != nil {
		return "", err
	}
	return fmt.Sprintf("%04o", st.Mode().Perm()), nil
}

// TightenDir sets path to 0700 if it exists with any other mode, and
// returns the previous mode as a string when it changed it.
func TightenDir(path string) (changed string, err error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if st.Mode().Perm() == PrivateDirMode {
		return "", nil
	}
	if err := os.Chmod(path, PrivateDirMode); err != nil {
		return "", err
	}
	return fmt.Sprintf("%04o", st.Mode().Perm()), nil
}
