//go:build unix

package queue

import (
	"os"
	"syscall"
)

func readUmask() os.FileMode {
	old := syscall.Umask(0)
	syscall.Umask(old)
	return os.FileMode(old)
}
