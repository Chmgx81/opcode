package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// ---- pure filter logic ------------------------------------------------

// seccompData is what the kernel shows a filter for one syscall.
type seccompData struct {
	nr, arch uint32
	arg0     uint64
}

// evalBPF interprets the classic-BPF subset the filter uses, so the
// program's decisions can be checked for inputs no live process could
// produce (a foreign arch, an x32 number, garbage in an argument's
// high word) without needing a kernel that allows them.
func evalBPF(t *testing.T, prog []unix.SockFilter, d seccompData) uint32 {
	t.Helper()
	var acc uint32
	pc := 0
	for steps := 0; steps < 10*len(prog)+10; steps++ {
		if pc < 0 || pc >= len(prog) {
			t.Fatalf("bpf: pc %d ran off the program", pc)
		}
		in := prog[pc]
		switch in.Code {
		case bpfLoadAbs:
			switch in.K {
			case offNr:
				acc = d.nr
			case offArch:
				acc = d.arch
			case offArg0:
				acc = uint32(d.arg0)
			case offArg0 + 4:
				acc = uint32(d.arg0 >> 32)
			default:
				t.Fatalf("bpf: load of unexpected offset %d", in.K)
			}
			pc++
		case bpfJmpEq:
			if acc == in.K {
				pc += 1 + int(in.Jt)
			} else {
				pc += 1 + int(in.Jf)
			}
		case bpfJmpSet:
			if acc&in.K != 0 {
				pc += 1 + int(in.Jt)
			} else {
				pc += 1 + int(in.Jf)
			}
		case bpfRetK:
			return in.K
		default:
			t.Fatalf("bpf: unexpected opcode %#x", in.Code)
		}
	}
	t.Fatal("bpf: program did not terminate")
	return 0
}

func TestNetworkFilterDecisions(t *testing.T) {
	prog, err := networkFilter()
	if err != nil {
		t.Fatalf("networkFilter: %v", err)
	}
	const (
		allow    = unix.SECCOMP_RET_ALLOW
		kill     = unix.SECCOMP_RET_KILL_PROCESS
		eafnosup = unix.SECCOMP_RET_ERRNO | uint32(unix.EAFNOSUPPORT)
		eperm    = unix.SECCOMP_RET_ERRNO | uint32(unix.EPERM)
		archI386 = 0x40000003
		archArm  = 0xc00000b7
		afPacket = 17
		afAlg    = 38
		afVsock  = 40
		afBt     = 31
		afTipc   = 30
		afXdp    = 44
	)
	sock := func(domain uint64) seccompData {
		return seccompData{nr: unix.SYS_SOCKET, arch: unix.AUDIT_ARCH_X86_64, arg0: domain}
	}
	call := func(nr uint32) seccompData {
		return seccompData{nr: nr, arch: unix.AUDIT_ARCH_X86_64}
	}
	tests := []struct {
		name string
		in   seccompData
		want uint32
	}{
		// Architecture and ABI: fail closed, never mis-evaluate.
		{"i386 socket", seccompData{nr: unix.SYS_SOCKET, arch: archI386, arg0: unix.AF_INET}, kill},
		{"i386 harmless", seccompData{nr: unix.SYS_READ, arch: archI386}, kill},
		{"aarch64", seccompData{nr: unix.SYS_SOCKET, arch: archArm, arg0: unix.AF_INET}, kill},
		{"x32 socket AF_INET", seccompData{nr: x32SyscallBit | unix.SYS_SOCKET, arch: unix.AUDIT_ARCH_X86_64, arg0: unix.AF_INET}, kill},
		{"x32 harmless", seccompData{nr: x32SyscallBit | unix.SYS_READ, arch: unix.AUDIT_ARCH_X86_64}, kill},

		// socket(2): only AF_UNIX and AF_NETLINK.
		{"AF_UNIX", sock(unix.AF_UNIX), allow},
		{"AF_NETLINK", sock(unix.AF_NETLINK), allow},
		{"AF_INET", sock(unix.AF_INET), eafnosup},
		{"AF_INET6", sock(unix.AF_INET6), eafnosup},
		{"AF_PACKET", sock(afPacket), eafnosup},
		{"AF_VSOCK", sock(afVsock), eafnosup},
		{"AF_BLUETOOTH", sock(afBt), eafnosup},
		{"AF_TIPC", sock(afTipc), eafnosup},
		{"AF_ALG", sock(afAlg), eafnosup},
		{"AF_XDP", sock(afXdp), eafnosup},
		{"AF_UNSPEC", sock(0), eafnosup},
		// The kernel truncates the domain to an int: a set high word
		// must neither hide AF_INET nor demote AF_UNIX.
		{"AF_INET with high garbage", sock(0xdeadbeef_00000000 | unix.AF_INET), eafnosup},
		{"AF_UNIX with high garbage", sock(0xffffffff_00000000 | unix.AF_UNIX), allow},

		// Sockets by another door.
		{"io_uring_setup", call(unix.SYS_IO_URING_SETUP), eperm},
		{"io_uring_enter", call(unix.SYS_IO_URING_ENTER), eperm},
		{"io_uring_register", call(unix.SYS_IO_URING_REGISTER), eperm},

		// Left to Landlock, which refuses ptrace-class access to
		// processes outside the sandbox (see the kernel test).
		{"ptrace", call(unix.SYS_PTRACE), allow},
		{"process_vm_readv", call(unix.SYS_PROCESS_VM_READV), allow},
		{"pidfd_getfd", call(unix.SYS_PIDFD_GETFD), allow},

		// Everything ordinary stays.
		{"read", call(unix.SYS_READ), allow},
		{"write", call(unix.SYS_WRITE), allow},
		{"execve", call(unix.SYS_EXECVE), allow},
		{"connect (AF_UNIX fds only exist post-socket)", call(unix.SYS_CONNECT), allow},
		{"socketpair", call(unix.SYS_SOCKETPAIR), allow},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := evalBPF(t, prog, tc.in); got != tc.want {
				t.Errorf("filter returned %#x, want %#x", got, tc.want)
			}
		})
	}
}

func TestAssembleRejectsBadPrograms(t *testing.T) {
	for name, prog := range map[string][]bpfInsn{
		"undefined label": {{code: bpfJmpEq, jt: "nowhere"}, {code: bpfRetK}},
		"backward jump":   {{label: "top", code: bpfRetK}, {code: bpfJmpEq, jt: "top"}},
		"duplicate label": {{label: "a", code: bpfRetK}, {label: "a", code: bpfRetK}},
	} {
		if _, err := assemble(prog); err == nil {
			t.Errorf("%s: assemble accepted it", name)
		}
	}
}

// ---- the real kernel --------------------------------------------------

// init makes the test binary double as the probe child: it optionally
// applies the full sandbox, performs one syscall named by
// OPCODE_SANDBOX_PROBE, and prints the errno. Running before flag
// parsing and TestMain, it never starts the test framework.
func init() {
	spec := os.Getenv("OPCODE_SANDBOX_PROBE")
	if spec == "" {
		return
	}
	// Both Landlock and seccomp bind to this thread; keep the probe
	// on it.
	runtime.LockOSThread()
	if os.Getenv("OPCODE_SANDBOX_APPLY") == "1" {
		if err := Apply([]string{os.Getenv("OPCODE_SANDBOX_WRITABLE")}); err != nil {
			fmt.Print("apply-err: ", err)
			os.Exit(2)
		}
	}
	fmt.Printf("errno=%d", int(runProbe(strings.Fields(spec))))
	os.Exit(0)
}

// Package-level so the kernel reads memory the Go runtime never
// moves.
var (
	probeBuf    [8]byte
	probeParams [120]byte // sizeof(struct io_uring_params)
	probeLocal  struct {
		base uintptr
		len  uint64
	}
	probeRemote struct {
		base uintptr
		len  uint64
	}
)

func errnoOf(err error) syscall.Errno {
	var e syscall.Errno
	if errors.As(err, &e) {
		return e
	}
	fmt.Print("probe: non-errno error: ", err)
	os.Exit(3)
	return 0
}

func runProbe(f []string) syscall.Errno {
	num := func(i int) uintptr {
		n, err := strconv.ParseUint(f[i], 10, 64)
		if err != nil {
			fmt.Print("probe: bad argument ", f[i])
			os.Exit(3)
		}
		return uintptr(n)
	}
	switch f[0] {
	case "socket":
		fd, _, e := syscall.Syscall(unix.SYS_SOCKET, num(1), num(2), num(3))
		if e == 0 {
			syscall.Close(int(fd))
		}
		return e
	case "x32socket":
		_, _, e := syscall.Syscall(x32SyscallBit|unix.SYS_SOCKET, unix.AF_INET, unix.SOCK_STREAM, 0)
		return e
	case "io_uring_setup":
		fd, _, e := syscall.Syscall(unix.SYS_IO_URING_SETUP, 1, uintptr(unsafe.Pointer(&probeParams[0])), 0)
		if e == 0 {
			syscall.Close(int(fd))
		}
		return e
	case "ptrace":
		pid := int(num(1))
		if err := syscall.PtraceAttach(pid); err != nil {
			return errnoOf(err)
		}
		var ws syscall.WaitStatus
		_, _ = syscall.Wait4(pid, &ws, syscall.WSTOPPED, nil)
		_ = syscall.PtraceDetach(pid)
		return 0
	case "pidfd_getfd":
		pfd, err := unix.PidfdOpen(int(num(1)), 0)
		if err != nil {
			return errnoOf(err)
		}
		fd, _, e := syscall.Syscall(unix.SYS_PIDFD_GETFD, uintptr(pfd), 0, 0)
		if e == 0 {
			syscall.Close(int(fd))
		}
		return e
	case "process_vm_readv":
		pid := int(num(1))
		maps, err := os.ReadFile(fmt.Sprintf("/proc/%d/maps", pid))
		if err != nil {
			return errnoOf(err)
		}
		start, _, _ := strings.Cut(string(maps), "-")
		addr, err := strconv.ParseUint(start, 16, 64)
		if err != nil {
			fmt.Print("probe: unparsable maps: ", start)
			os.Exit(3)
		}
		probeLocal.base = uintptr(unsafe.Pointer(&probeBuf[0]))
		probeLocal.len = uint64(len(probeBuf))
		probeRemote.base = uintptr(addr)
		probeRemote.len = uint64(len(probeBuf))
		_, _, e := syscall.Syscall6(unix.SYS_PROCESS_VM_READV, uintptr(pid),
			uintptr(unsafe.Pointer(&probeLocal)), 1,
			uintptr(unsafe.Pointer(&probeRemote)), 1, 0)
		return e
	}
	fmt.Print("probe: unknown probe ", f[0])
	os.Exit(3)
	return 0
}

// runChild runs one probe in a fresh process, with or without the full
// sandbox applied, and returns its reported errno — or the fatal
// signal that ended it.
func runChild(t *testing.T, apply bool, spec string) (errno syscall.Errno, sig syscall.Signal) {
	t.Helper()
	applyEnv := "OPCODE_SANDBOX_APPLY=0"
	if apply {
		applyEnv = "OPCODE_SANDBOX_APPLY=1"
	}
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(),
		"OPCODE_SANDBOX_PROBE="+spec,
		"OPCODE_SANDBOX_WRITABLE="+t.TempDir(),
		applyEnv)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 0, ws.Signal()
		}
		t.Fatalf("probe %q (apply=%v): %v: %s", spec, apply, err, out)
	} else if err != nil {
		t.Fatalf("probe %q: %v", spec, err)
	}
	s, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "errno=")
	if !ok {
		t.Fatalf("probe %q: unexpected output %q", spec, out)
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		t.Fatalf("probe %q: bad errno %q", spec, s)
	}
	return syscall.Errno(n), 0
}

// anyDenial in a kernel test means any nonzero errno counts as a
// refusal.
const anyDenial = syscall.Errno(0xffff)

func requireSeccompHost(t *testing.T) {
	t.Helper()
	if !Supported() {
		t.Skip("kernel without landlock")
	}
	if !confinedNetwork() {
		t.Skip("seccomp filter is x86_64-only")
	}
}

// Every probe is run twice: once unconfined (the control — the host
// must actually allow the call, or the confined result proves
// nothing, and the case is skipped with the reason) and once inside
// the real sandbox.
func TestSeccompKernelEnforcement(t *testing.T) {
	requireSeccompHost(t)

	victim := exec.Command("sleep", "60")
	if err := victim.Start(); err != nil {
		t.Skipf("cannot start a victim process: %v", err)
	}
	t.Cleanup(func() { _ = victim.Process.Kill(); _ = victim.Wait() })
	pid := strconv.Itoa(victim.Process.Pid)

	tests := []struct {
		name  string
		probe string
		want  syscall.Errno // confined result; 0 = must still work; anyDenial = any nonzero errno
	}{
		{"AF_INET", "socket 2 1 0", unix.EAFNOSUPPORT},
		{"AF_INET6", "socket 10 1 0", unix.EAFNOSUPPORT},
		{"AF_INET with high garbage", "socket " + strconv.FormatUint(0x1_0000_0002, 10) + " 1 0", unix.EAFNOSUPPORT},
		{"AF_VSOCK", "socket 40 1 0", unix.EAFNOSUPPORT},
		{"AF_UNIX stays", "socket 1 1 0", 0},
		{"AF_NETLINK stays", "socket 16 3 0", 0},
		{"io_uring_setup", "io_uring_setup", unix.EPERM},
		// Landlock's own ptrace hook, not the seccomp filter: a
		// same-user process outside the sandbox must stay out of
		// reach, or it is a way to borrow its unconfined network.
		{"ptrace a neighbour", "ptrace " + pid, anyDenial},
		{"pidfd_getfd from a neighbour", "pidfd_getfd " + pid, anyDenial},
		{"process_vm_readv from a neighbour", "process_vm_readv " + pid, anyDenial},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if ctl, sig := runChild(t, false, tc.probe); sig != 0 {
				t.Fatalf("control was killed by %v", sig)
			} else if tc.want != 0 && ctl != 0 {
				t.Skipf("host already refuses this unconfined (%v), nothing to compare", ctl)
			} else if tc.want == 0 && ctl != 0 {
				t.Skipf("host refuses this even unconfined (%v)", ctl)
			}
			got, sig := runChild(t, true, tc.probe)
			if sig != 0 {
				t.Fatalf("confined probe was killed by %v", sig)
			}
			if tc.want == anyDenial {
				if got == 0 {
					t.Errorf("the confined call succeeded; it must be refused")
				}
			} else if got != tc.want {
				t.Errorf("confined errno = %v (%d), want %v (%d)", got, int(got), tc.want, int(tc.want))
			}
		})
	}
}

// An x32-ABI socket call reports AUDIT_ARCH_X86_64 but a number that
// no plain compare matches. Unconfined it is merely ENOSYS (or works,
// where the kernel has x32); confined, the process must die.
func TestSeccompKillsX32Syscalls(t *testing.T) {
	requireSeccompHost(t)
	if _, sig := runChild(t, false, "x32socket"); sig != 0 {
		t.Fatalf("control was killed by %v", sig)
	}
	_, sig := runChild(t, true, "x32socket")
	if sig != syscall.SIGSYS {
		t.Errorf("confined x32 syscall: signal = %v, want SIGSYS", sig)
	}
}
