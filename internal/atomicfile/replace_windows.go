//go:build windows

package atomicfile

import (
	"syscall"
	"unsafe"
)

const moveFileExReplaceExisting = 0x1

var moveFileEx = syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")

func replace(source, destination string) error {
	sourcePtr, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	result, _, callErr := moveFileEx.Call(
		uintptr(unsafe.Pointer(sourcePtr)),
		uintptr(unsafe.Pointer(destinationPtr)),
		moveFileExReplaceExisting,
	)
	if result == 0 {
		if callErr == nil {
			return syscall.EINVAL
		}
		return callErr
	}
	return nil
}
