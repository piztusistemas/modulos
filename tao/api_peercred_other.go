//go:build !linux

package main

import "net"

// orixeRecoñecidaAPI has no OS-level peer-credential check on this platform
// (SO_PEERCRED is Linux-specific; macOS/Windows would need a different,
// unimplemented mechanism — see docs/api-tao.md §3). Falls back to the
// socket file's own permission (0600, same OS user) as the only guarantee,
// same as before this hardening existed for Linux.
func orixeRecoñecidaAPI(conn *net.UnixConn, baseDir, taoModulosDir string) bool {
	return true
}
