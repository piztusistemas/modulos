//go:build linux

package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// orixeRecoñecidaAPI is the real defense against "ferramentas alleas ao
// ecosistema Piztu" (see docs/api-tao.md §3): a bearer token alone is not
// enough, because any process running as this same OS user can read
// tao-api.json (0600 blocks other USERS, not other PROGRAMS of this same
// user) and present it. SO_PEERCRED asks the kernel — not the connecting
// process itself — who is really on the other end of this specific Unix
// socket connection: its PID, from which /proc/<pid>/exe gives the real
// absolute path of the binary that opened it. That path can't be spoofed by
// merely knowing a secret string.
//
// A connection is accepted only if the peer runs as this same UID AND its
// executable lives inside a recognized tree: (when baseDir is known — Tao
// was launched as part of a real Piztu install, see contexto_piztu.go) the
// piztu binary itself or any módulo under baseDir/modulos/; OR (when
// taoModulosDir is known — Tao always knows this, see taoModulosDir() in
// modulos_tao.go) any external módulo DE Tao under taoModulosDir. Without a
// known baseDir/taoModulosDir (standalone/dev run) only the UID check
// applies — the same guarantee this API had before this hardening.
func orixeRecoñecidaAPI(conn *net.UnixConn, baseDir, taoModulosDir string) bool {
	cred, err := peerCredAPI(conn)
	if err != nil {
		fmt.Println("aviso: api local de Tao non puido verificar a orixe da conexión:", err)
		return false
	}
	if int(cred.Uid) != os.Getuid() {
		fmt.Println("aviso: api local de Tao rexeitou unha conexión doutro usuario (uid", cred.Uid, ")")
		return false
	}
	if baseDir == "" && taoModulosDir == "" {
		return true // nin instalación de Piztu nin cartafol de módulos de Tao coñecidos: só se garante o de sempre (mesmo usuario)
	}
	exe, err := os.Readlink("/proc/" + strconv.Itoa(int(cred.Pid)) + "/exe")
	if err != nil {
		fmt.Println("aviso: api local de Tao non puido resolver o binario chamador (pid", cred.Pid, "):", err)
		return false
	}
	if !exeDentroDoEcosistemaAPI(exe, baseDir, taoModulosDir) {
		fmt.Println("aviso: api local de Tao rexeitou unha conexión dun binario alleo ao ecosistema Piztu:", exe)
		return false
	}
	return true
}

func peerCredAPI(conn *net.UnixConn) (*syscall.Ucred, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return nil, err
	}
	var cred *syscall.Ucred
	var gerr error
	if err := raw.Control(func(fd uintptr) {
		cred, gerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return nil, err
	}
	return cred, gerr
}

// exeDentroDoEcosistemaAPI reports whether exe is piztu itself, or a
// módulo executable, under the Piztu install tree baseDir — OR an external
// módulo DE Tao under taoModulosDir (either may be "" if unknown; each is
// checked independently). Deliberately a path-membership check, not a
// per-módulo modulo.json lookup: Tao stays independent of piztu's module
// registry (see CLAUDE.md, Xesta/Tao isolation) — it only needs to know
// "this binary was placed inside a protected install tree", which assumes
// that tree is not writable by the unprivileged account that runs Tao day
// to day (true for a real install: /opt/piztu belongs to the
// installer/updater, and taoModulosDir the same way it protects Tao's own
// executable).
func exeDentroDoEcosistemaAPI(exe, baseDir, taoModulosDir string) bool {
	exe = filepath.Clean(exe)
	if baseDir != "" {
		base, err := filepath.Abs(baseDir)
		if err == nil {
			if exe == filepath.Join(base, "piztu") {
				return true
			}
			if dentroDeArbore(exe, filepath.Join(base, "modulos")) {
				return true
			}
		}
	}
	if taoModulosDir != "" && dentroDeArbore(exe, taoModulosDir) {
		return true
	}
	return false
}

// dentroDeArbore reports whether exe (an absolute, cleaned path) sits
// strictly inside raiz — not raiz itself.
func dentroDeArbore(exe, raiz string) bool {
	raizAbs, err := filepath.Abs(raiz)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(raizAbs, exe)
	if err != nil || rel == "." || rel == ".." {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
