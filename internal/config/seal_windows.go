//go:build windows

package config

import (
	"errors"
	"syscall"
	"unsafe"
)

var (
	crypt32            = syscall.NewLazyDLL("crypt32.dll")
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procCryptProtect   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotect = crypt32.NewProc("CryptUnprotectData")
	procLocalFree      = kernel32.NewProc("LocalFree")
)

type dataBlob struct {
	cbData uint32
	pbData *byte
}

func newBlob(b []byte) dataBlob {
	if len(b) == 0 {
		return dataBlob{}
	}
	return dataBlob{cbData: uint32(len(b)), pbData: &b[0]}
}

func (b dataBlob) bytes() []byte {
	if b.pbData == nil || b.cbData == 0 {
		return nil
	}
	out := make([]byte, b.cbData)
	copy(out, unsafe.Slice(b.pbData, b.cbData))
	return out
}

func (b dataBlob) free() {
	if b.pbData != nil {
		procLocalFree.Call(uintptr(unsafe.Pointer(b.pbData)))
	}
}

func seal(plain []byte) ([]byte, error) {
	in := newBlob(plain)
	var out dataBlob

	r, _, err := procCryptProtect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer out.free()

	return out.bytes(), nil
}

func unseal(sealed []byte) ([]byte, error) {
	if len(sealed) == 0 {
		return nil, errors.New("empty credential")
	}

	in := newBlob(sealed)
	var out dataBlob

	r, _, err := procCryptUnprotect.Call(
		uintptr(unsafe.Pointer(&in)),
		0, 0, 0, 0,
		0,
		uintptr(unsafe.Pointer(&out)),
	)
	if r == 0 {
		return nil, err
	}
	defer out.free()

	return out.bytes(), nil
}
