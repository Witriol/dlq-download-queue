//go:build !unix

package queue

import "os"

// No umask outside Unix; extraction leaves modes as 7z wrote them.
func readUmask() os.FileMode {
	return groupOtherWrite
}
