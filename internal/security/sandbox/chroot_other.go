//go:build !linux

package sandbox

import "fmt"

// NewChroot n'est supporté que sur Linux.
func NewChroot(_ Config) (Sandbox, error) {
	return nil, fmt.Errorf("chroot sandbox: only supported on Linux")
}
