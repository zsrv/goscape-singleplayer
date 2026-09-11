//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// Console wiring for the GUI-subsystem Windows build.
//
// Linking with -H windowsgui is what stops Explorer from opening a console
// window next to the game window: the PE subsystem field says GUI, so the
// loader allocates no console. The cost is that the process then has no
// standard handles of its own, and every write in this command — the server
// log, the flag package's usage text, a panic trace — would go to a NULL
// handle and vanish. setupConsole is what gives them back.
//
// Three launch shapes, in the order it tries them:
//
//   - Handles already present. A redirect or a pipe supplies them through
//     STARTUPINFO, which the GUI subsystem does not clear: `prog.exe -version
//     > out.txt`, and the release workflow's capture of -version, both land
//     here. Nothing to do, and nothing that may be touched — the handles are
//     where the user pointed them.
//   - A parent console. Started from cmd.exe, PowerShell or a Git bash shell
//     that owns a console: AttachConsole borrows that terminal, so running
//     from a shell behaves exactly as a console-subsystem build did. Without
//     this, -H windowsgui would make the binary mute in every shell, which is
//     a worse bug than the one it fixes.
//   - No console at all. Explorer, a shortcut, a file association. This is the
//     only case where -console means anything, and the only one that ends with
//     AllocConsole.
//
// AttachConsole and AllocConsole are not in x/sys/windows, so they are resolved
// by hand. NewLazySystemDLL rather than syscall.NewLazyDLL: it resolves only
// out of the system directory, closing the DLL-planting hole that loading a
// bare "kernel32.dll" would leave open for a binary started from a
// user-writable directory such as Downloads.
var (
	kernel32          = windows.NewLazySystemDLL("kernel32.dll")
	procAttachConsole = kernel32.NewProc("AttachConsole")
	procAllocConsole  = kernel32.NewProc("AllocConsole")
)

// attachParentProcess is ATTACH_PARENT_PROCESS, defined as (DWORD)-1.
const attachParentProcess = ^uintptr(0)

// setupConsole gives the process usable standard streams where it can, and
// reports whether standard output ended up usable.
//
// Standard output is what decides the return value, because it is what the
// fallback log is a fallback for: server.NewLogger writes to os.Stdout.
func setupConsole(want bool) bool {
	needOut := stdHandleMissing(windows.STD_OUTPUT_HANDLE)
	needErr := stdHandleMissing(windows.STD_ERROR_HANDLE)
	if !needOut && !needErr {
		return true
	}

	if !attachOrAllocConsole(want) {
		return !needOut
	}

	// Bind only what was missing. `prog.exe > log.txt` from a terminal keeps
	// the shell's file for output and gets the console for errors, which is
	// what was asked for in each case.
	outOK := !needOut
	if needOut {
		outOK = bindConsole(windows.STD_OUTPUT_HANDLE, &os.Stdout)
	}
	if needErr {
		// Best effort: a usable stdout is the contract, and diverting a whole
		// run's log to a file because stderr alone could not be opened would
		// be a worse trade than losing stderr.
		_ = bindConsole(windows.STD_ERROR_HANDLE, &os.Stderr)
	}
	return outOK
}

// stdHandleMissing reports whether a standard handle is one the process cannot
// write to. A GUI-subsystem process with no console gets NULL, which is not an
// error; an explicitly invalid handle is reported as one.
func stdHandleMissing(which uint32) bool {
	h, err := windows.GetStdHandle(which)
	return err != nil || h == 0 || h == windows.InvalidHandle
}

func attachOrAllocConsole(want bool) bool {
	if callBool(procAttachConsole, attachParentProcess) {
		return true
	}
	return want && callBool(procAllocConsole)
}

// callBool invokes a kernel32 entry point returning a BOOL, treating an
// entry point that cannot be resolved as failure rather than a panic:
// LazyProc.Call panics when the procedure is missing, and this runs before the
// window opens, where a panic would be both fatal and — with no console yet —
// invisible.
func callBool(p *windows.LazyProc, args ...uintptr) bool {
	if err := p.Find(); err != nil {
		return false
	}
	r, _, _ := p.Call(args...)
	return r != 0
}

// bindConsole points one standard handle at the console and rebuilds the
// matching os.File.
//
// Both halves are needed and neither substitutes for the other:
//
//   - SetStdHandle is what the Go runtime sees. runtime.write1 resolves fd 1
//     and fd 2 through GetStdHandle on every single write
//     (runtime/os_windows.go), and never consults os.Stdout or os.Stderr — so
//     this call, not the assignment below, is what makes a panic trace or a
//     fatal runtime error reach the console. That matters here specifically:
//     runWindow re-panics on a GLFW failure precisely so the operator sees
//     why, and without this the trace would be written to nothing.
//   - The os.File assignment is what Go code sees: server.NewLogger's
//     os.Stdout argument, and every fmt.Fprintf(os.Stderr, ...) in main.
//
// os.NewFile is doing more than wrapping a handle. It calls GetConsoleMode and,
// for a console, routes writes through WriteConsole with UTF-16 conversion
// instead of WriteFile, so non-ASCII output is not mangled by whatever code
// page the console happens to be in.
func bindConsole(which uint32, target **os.File) bool {
	const name = "CONOUT$"
	h, err := windows.CreateFile(windows.StringToUTF16Ptr(name),
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil, windows.OPEN_EXISTING, 0, 0)
	if err != nil {
		return false
	}
	if err := windows.SetStdHandle(which, h); err != nil {
		_ = windows.CloseHandle(h)
		return false
	}
	*target = os.NewFile(uintptr(h), name)
	return true
}

// redirectStdio sends both output streams to f, runtime writes included, for
// the one launch shape with no console to bind: a GUI build started from
// Explorer without -console. See bindConsole for why SetStdHandle and the
// assignments are both required.
//
// Standard input is deliberately left alone. Nothing in this command or the
// client reads it, and CONIN$ would have to be opened read-write to be useful,
// which is surface for no gain.
func redirectStdio(f *os.File) {
	h := windows.Handle(f.Fd())
	_ = windows.SetStdHandle(windows.STD_OUTPUT_HANDLE, h)
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, h)
	os.Stdout, os.Stderr = f, f
}
