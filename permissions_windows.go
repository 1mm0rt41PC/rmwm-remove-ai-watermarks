//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	securityDLL                  = syscall.NewLazyDLL("advapi32.dll")
	getFileSecurityW             = securityDLL.NewProc("GetFileSecurityW")
	getSecurityDescriptorControl = securityDLL.NewProc("GetSecurityDescriptorControl")
	setFileSecurityW             = securityDLL.NewProc("SetFileSecurityW")
)

const (
	daclSecurityInformation         uint32 = 0x00000004
	protectedDACLInformation        uint32 = 0x80000000
	unprotectedDACLInformation      uint32 = 0x20000000
	securityDescriptorDACLProtected uint16 = 0x1000
	maximumDACLDescriptorSize              = 1 << 20
)

// protectNewFile copies the source DACL and its inheritance protection before
// the caller writes any data into a newly created file. Windows chmod only
// changes the read-only attribute; it cannot protect backup or temporary data.
// The open file must remain open until after this function returns.
//
// Only the DACL is copied: owner, SACL and encryption are outside this helper's
// scope. A new stdout/stdin destination has no source DACL to copy.
func protectNewFile(file *os.File, sourcePath string) error {
	if sourcePath == "" {
		return nil
	}
	if file == nil {
		return fmt.Errorf("protection du nouveau fichier : fichier nil")
	}
	source, err := widePath(sourcePath)
	if err != nil {
		return fmt.Errorf("chemin source pour lecture de la DACL : %w", err)
	}
	target, err := widePath(file.Name())
	if err != nil {
		return fmt.Errorf("chemin cible pour copie de la DACL : %w", err)
	}

	// GetFileSecurityW returns a self-relative SECURITY_DESCRIPTOR. First ask
	// for its exact size; DACL access requires READ_CONTROL, not SACL privilege.
	var needed uint32
	ok, _, callErr := getFileSecurityW.Call(
		uintptr(unsafe.Pointer(source)), uintptr(daclSecurityInformation),
		0, 0, uintptr(unsafe.Pointer(&needed)),
	)
	if ok == 0 && callErr != syscall.ERROR_INSUFFICIENT_BUFFER {
		return fmt.Errorf("lecture de la taille DACL source : %w", callErr)
	}
	// A DACL-only descriptor is small (the ACL size field itself is 16-bit).
	// Bound the allocation and reject a malformed size before dereferencing.
	if needed == 0 || needed > maximumDACLDescriptorSize {
		return fmt.Errorf("taille du descripteur DACL source invalide : %d", needed)
	}
	descriptor := make([]byte, int(needed))
	ok, _, callErr = getFileSecurityW.Call(
		uintptr(unsafe.Pointer(source)), uintptr(daclSecurityInformation),
		uintptr(unsafe.Pointer(&descriptor[0])), uintptr(len(descriptor)),
		uintptr(unsafe.Pointer(&needed)),
	)
	if ok == 0 {
		return fmt.Errorf("lecture de la DACL source : %w", callErr)
	}

	// SECURITY_DESCRIPTOR_CONTROL is a WORD; the revision output is a DWORD.
	var control uint16
	var revision uint32
	ok, _, callErr = getSecurityDescriptorControl.Call(
		uintptr(unsafe.Pointer(&descriptor[0])),
		uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)),
	)
	if ok == 0 {
		return fmt.Errorf("lecture de la protection DACL source : %w", callErr)
	}
	information := daclSecurityInformation | unprotectedDACLInformation
	if control&securityDescriptorDACLProtected != 0 {
		information = daclSecurityInformation | protectedDACLInformation
	}
	ok, _, callErr = setFileSecurityW.Call(
		uintptr(unsafe.Pointer(target)), uintptr(information),
		uintptr(unsafe.Pointer(&descriptor[0])),
	)
	runtime.KeepAlive(descriptor)
	runtime.KeepAlive(file)
	if ok == 0 {
		return fmt.Errorf("copie de la DACL avant écriture : %w", callErr)
	}
	return nil
}
