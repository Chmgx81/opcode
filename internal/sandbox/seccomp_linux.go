//go:build linux

package sandbox

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// confinedNetwork reports whether Apply also blocks network sockets
// here. The filter is hand-written x86_64 classic BPF, so other
// architectures keep the file-confinement only — Status() must not
// claim the network half where it cannot run.
func confinedNetwork() bool { return runtime.GOARCH == "amd64" }

// BPF opcodes the filter needs. x/sys/unix ships the constants but
// no instruction builders, so compose them by hand.
const (
	bpfLoadAbs = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	bpfJmpEq   = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	bpfJmpSet  = unix.BPF_JMP | unix.BPF_JSET | unix.BPF_K
	bpfRetK    = unix.BPF_RET | unix.BPF_K
)

// x32SyscallBit marks an x32-ABI syscall number. x32 processes report
// AUDIT_ARCH_X86_64 too, so the arch check alone lets them through —
// and their socket(2) is 0x40000000|41, which no plain
// `nr == SYS_SOCKET` compare matches.
const x32SyscallBit = 0x40000000

// seccomp_data offsets (uapi/linux/seccomp.h): nr at 0, arch at 4,
// args[0] at 16. Arguments are 64-bit; amd64 is little-endian, so
// offset 16 is the low word — exactly what the kernel's `int domain`
// parameter is truncated to, whatever garbage sits in the high word.
const (
	offNr   = 0
	offArch = 4
	offArg0 = 16
)

// bpfInsn is one instruction with symbolic jump targets, resolved by
// assemble. An empty target falls through to the next instruction.
type bpfInsn struct {
	label  string // names this instruction as a jump target
	code   uint16
	k      uint32
	jt, jf string
}

// assemble resolves labels into classic-BPF relative offsets. Classic
// BPF only jumps forward, by at most 255 instructions; anything else is
// a bug in the program, reported rather than mis-encoded.
func assemble(prog []bpfInsn) ([]unix.SockFilter, error) {
	at := make(map[string]int, len(prog))
	for i, in := range prog {
		if in.label == "" {
			continue
		}
		if _, dup := at[in.label]; dup {
			return nil, fmt.Errorf("bpf: duplicate label %q", in.label)
		}
		at[in.label] = i
	}
	offset := func(from int, label string) (uint8, error) {
		if label == "" {
			return 0, nil
		}
		to, ok := at[label]
		if !ok {
			return 0, fmt.Errorf("bpf: undefined label %q", label)
		}
		d := to - (from + 1)
		if d < 0 || d > 255 {
			return 0, fmt.Errorf("bpf: jump to %q is out of range (%d)", label, d)
		}
		return uint8(d), nil
	}
	out := make([]unix.SockFilter, len(prog))
	for i, in := range prog {
		jt, err := offset(i, in.jt)
		if err != nil {
			return nil, err
		}
		jf, err := offset(i, in.jf)
		if err != nil {
			return nil, err
		}
		out[i] = unix.SockFilter{Code: in.code, Jt: jt, Jf: jf, K: in.k}
	}
	return out, nil
}

// networkFilter builds the seccomp program. In order:
//
//  1. Any architecture other than x86_64, and any x32-ABI syscall,
//     kills the process. Syscall numbers mean something else there
//     (a 32-bit binary exec'd from the sandbox inherits this filter),
//     so failing closed beats mis-evaluating.
//  2. socket(2) is allowed for AF_UNIX and AF_NETLINK only; every
//     other family fails with EAFNOSUPPORT. An allowlist, not a list
//     of bad families: AF_INET, AF_INET6 and AF_PACKET are the
//     obvious routes out, but AF_VSOCK (host<->VM), AF_BLUETOOTH,
//     AF_TIPC, AF_RDS, AF_XDP and friends are routes too, and new
//     families appear with new kernels. AF_UNIX keeps local pipes and
//     daemons working. AF_NETLINK stays because getifaddrs, ip, ss
//     and Go's net package use it — it talks to the local kernel and
//     cannot carry data off the machine without CAP_NET_ADMIN, which
//     no_new_privs withholds.
//  3. io_uring_{setup,enter,register} fail with EPERM: IORING_OP_SOCKET
//     and IORING_OP_CONNECT open and use sockets without ever calling
//     socket(2), so leaving io_uring open would void step 2 on any
//     kernel new enough to have them (5.19+).
//
// ptrace, process_vm_readv/writev and pidfd_getfd are deliberately NOT
// filtered. They would be the way out — attach to a same-user process
// outside the sandbox and borrow its network — but Landlock itself
// refuses ptrace-class access to any process outside the caller's
// domain (with Yama's ptrace_scope at 0 too); the live tests prove it
// on the running kernel. Filtering them as well would only break
// strace, gdb and dlv on the sandbox's own children.
//
// Not closed by any of this: connecting to an existing AF_UNIX socket
// path (Landlock does not mediate connect on pathname sockets), so a
// reachable docker.sock, ssh-agent or systemd-resolved varlink socket
// still leads out. Blocking AF_UNIX would break too much to be
// the default.
//
// socketpair(2) is left alone: it only ever connects two endpoints in
// this same host, and the kernel refuses it for the inet families.
func networkFilter() ([]unix.SockFilter, error) {
	deny := func(errno syscall.Errno) uint32 { return unix.SECCOMP_RET_ERRNO | uint32(errno) }
	prog := []bpfInsn{
		{code: bpfLoadAbs, k: offArch},
		{code: bpfJmpEq, k: unix.AUDIT_ARCH_X86_64, jf: "kill"},
		{code: bpfLoadAbs, k: offNr},
		{code: bpfJmpSet, k: x32SyscallBit, jt: "kill"},
		{code: bpfJmpEq, k: unix.SYS_SOCKET, jt: "socket"},
		{code: bpfJmpEq, k: unix.SYS_IO_URING_SETUP, jt: "eperm"},
		{code: bpfJmpEq, k: unix.SYS_IO_URING_ENTER, jt: "eperm"},
		{code: bpfJmpEq, k: unix.SYS_IO_URING_REGISTER, jt: "eperm"},

		{code: bpfRetK, k: unix.SECCOMP_RET_ALLOW},

		{label: "socket", code: bpfLoadAbs, k: offArg0},
		{code: bpfJmpEq, k: unix.AF_UNIX, jt: "allow"},
		{code: bpfJmpEq, k: unix.AF_NETLINK, jt: "allow"},
		{code: bpfRetK, k: deny(unix.EAFNOSUPPORT)},

		{label: "allow", code: bpfRetK, k: unix.SECCOMP_RET_ALLOW},
		{label: "eperm", code: bpfRetK, k: deny(unix.EPERM)},
		{label: "kill", code: bpfRetK, k: unix.SECCOMP_RET_KILL_PROCESS},
	}
	return assemble(prog)
}

// denyNetwork installs the filter on the calling thread. Landlock
// sees files, not sockets, so without it a sandboxed command could
// read the project and send it anywhere (audit S2). Apply calls this
// after landlock_restrict_self. NO_NEW_PRIVS is (re)asserted here —
// idempotent, and what lets an unprivileged process install a filter
// at all — so the ordering never depends on the caller. Every failure
// is returned and Apply passes it up: a half-applied sandbox must stop
// the command, not run it. Off x86_64 no filter is built and the
// network stays open (Status() says nothing about it there): the arch
// check would kill every legitimate process, and a silent allow would
// be worse than an honest absence.
func denyNetwork() error {
	if !confinedNetwork() {
		return nil
	}
	filter, err := networkFilter()
	if err != nil {
		return err
	}
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", err)
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}
	// syscall.Syscall, not unix.Prctl: the kernel reads prog through
	// a pointer passed as uintptr, and only the syscall package's
	// entry points keep that operand alive and unmoved for the call.
	if _, _, errno := syscall.Syscall(syscall.SYS_PRCTL, unix.PR_SET_SECCOMP,
		unix.SECCOMP_MODE_FILTER, uintptr(unsafe.Pointer(&prog))); errno != 0 {
		return fmt.Errorf("prctl(PR_SET_SECCOMP): %w", errno)
	}
	return nil
}
