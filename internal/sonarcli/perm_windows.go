//go:build windows

package sonarcli

import (
	"fmt"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// PrivateFileLabel is the short description of a private credentials file
// shown by `sqa doctor`.
func PrivateFileLabel() string { return "restricted to your user account" }

// PrivateDirLabel is the short description of a private directory shown by
// `sqa doctor`.
func PrivateDirLabel() string { return "restricted to your user account" }

// TightenFile applies a protected DACL on path granting only the current
// user full control. It reports a change when the ACL was not already
// restricted that way.
func TightenFile(path string) (changed string, err error) {
	return tighten(path, false)
}

// TightenDir is TightenFile for a directory (child objects inherit the ACL).
func TightenDir(path string) (changed string, err error) {
	return tighten(path, true)
}

func tighten(path string, dir bool) (changed string, err error) {
	ok, err := isRestrictedToCurrentUser(path)
	if err != nil {
		return "", err
	}
	if err := restrictToCurrentUser(path, dir); err != nil {
		return "", err
	}
	if ok {
		return "", nil
	}
	return "not restricted to your user account", nil
}

func currentUserSID() (*windows.SID, error) {
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return nil, fmt.Errorf("current user SID: %w", err)
	}
	return tu.User.Sid, nil
}

func restrictToCurrentUser(path string, dir bool) error {
	sid, err := currentUserSID()
	if err != nil {
		return err
	}
	inherit := uint32(windows.NO_INHERITANCE)
	if dir {
		inherit = windows.CONTAINER_INHERIT_ACE | windows.OBJECT_INHERIT_ACE
	}
	var pinner runtime.Pinner
	pinner.Pin(sid)
	defer pinner.Unpin()
	ea := []windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inherit,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(sid),
		},
	}}
	acl, err := windows.ACLFromEntries(ea, nil)
	if err != nil {
		return fmt.Errorf("build ACL for %s: %w", path, err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
		sid,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("set ACL on %s: %w", path, err)
	}
	return nil
}

func isRestrictedToCurrentUser(path string) (bool, error) {
	sid, err := currentUserSID()
	if err != nil {
		return false, err
	}
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION,
	)
	if err != nil {
		return false, fmt.Errorf("read ACL of %s: %w", path, err)
	}
	ctrl, _, err := sd.Control()
	if err != nil {
		return false, err
	}
	if ctrl&windows.SE_DACL_PROTECTED == 0 {
		return false, nil
	}
	dacl, _, err := sd.DACL()
	if err != nil || dacl == nil {
		return false, err
	}
	if dacl.AceCount != 1 {
		return false, nil
	}
	var ace *windows.ACCESS_ALLOWED_ACE
	if err := windows.GetAce(dacl, 0, &ace); err != nil || ace == nil {
		return false, nil
	}
	if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
		return false, nil
	}
	aceSid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
	return windows.EqualSid(sid, aceSid), nil
}
