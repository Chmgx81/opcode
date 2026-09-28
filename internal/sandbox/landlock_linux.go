//go:build linux

package sandbox

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Supported reports whether this kernel implements Landlock. The probe
// is a create_ruleset call with the VERSION flag, which installs
// nothing — it just returns the highest supported ABI version.
func Supported() bool {
	_, err := ProbeABI()
	return err == nil
}

// ProbeABI returns the kernel's highest Landlock ABI version
// (1 on 5.13, 2 on 5.19, 3 on 6.2, 4 on 6.10, 5 on 6.12+).
func ProbeABI() (int, error) {
	v, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		0, 0, unix.LANDLOCK_CREATE_RULESET_VERSION)
	if errno != 0 {
		return 0, fmt.Errorf("%w: landlock_create_ruleset: %v", ErrUnsupported, errno)
	}
	return int(v), nil
}

// rightsForABI returns the full access set handled at this ABI: every
// right the kernel knows must be handled, or the unhandled ones stay
// unrestricted — handling is what confines.
func rightsForABI(abi int) uint64 {
	r := uint64(unix.LANDLOCK_ACCESS_FS_EXECUTE | unix.LANDLOCK_ACCESS_FS_WRITE_FILE |
		unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR |
		unix.LANDLOCK_ACCESS_FS_REMOVE_DIR | unix.LANDLOCK_ACCESS_FS_REMOVE_FILE |
		unix.LANDLOCK_ACCESS_FS_MAKE_CHAR | unix.LANDLOCK_ACCESS_FS_MAKE_DIR |
		unix.LANDLOCK_ACCESS_FS_MAKE_REG | unix.LANDLOCK_ACCESS_FS_MAKE_SOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_FIFO | unix.LANDLOCK_ACCESS_FS_MAKE_BLOCK |
		unix.LANDLOCK_ACCESS_FS_MAKE_SYM)
	if abi >= 2 {
		r |= unix.LANDLOCK_ACCESS_FS_REFER
	}
	if abi >= 3 {
		r |= unix.LANDLOCK_ACCESS_FS_TRUNCATE
	}
	if abi >= 4 {
		r |= unix.LANDLOCK_ACCESS_FS_IOCTL_DEV
	}
	return r
}

const rightReadOnly = unix.LANDLOCK_ACCESS_FS_EXECUTE |
	unix.LANDLOCK_ACCESS_FS_READ_FILE | unix.LANDLOCK_ACCESS_FS_READ_DIR

// Apply installs the Landlock ruleset on this thread: read+execute
// beneath "/" (everything), full access beneath each writable root
// and /dev/null, then no_new_privs and restrict_self. It must run in
// a process that has not yet spawned threads — the __sandbox child.
func Apply(writable []string) error {
	abi, err := ProbeABI()
	if err != nil {
		return err
	}
	handled := rightsForABI(abi)

	attr := unix.LandlockRulesetAttr{Access_fs: handled}
	fd, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_CREATE_RULESET,
		uintptr(unsafe.Pointer(&attr)), unsafe.Sizeof(attr), 0)
	if errno != 0 {
		return fmt.Errorf("landlock_create_ruleset: %v", errno)
	}
	defer syscall.Close(int(fd))

	addRule := func(dir string, access uint64) error {
		// O_PATH is the landlock-open convention: the fd references
		// the path without opening the file itself.
		f, err := os.OpenFile(dir, unix.O_PATH|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", dir, err)
		}
		defer f.Close()
		rule := unix.LandlockPathBeneathAttr{Allowed_access: access, Parent_fd: int32(f.Fd())}
		_, _, errno := syscall.Syscall6(unix.SYS_LANDLOCK_ADD_RULE, fd,
			unix.LANDLOCK_RULE_PATH_BENEATH,
			uintptr(unsafe.Pointer(&rule)), 0, 0, 0)
		if errno != 0 {
			return fmt.Errorf("landlock_add_rule(%s): %v", dir, errno)
		}
		return nil
	}
	// Full access is bounded by the handled set; mask it per ABI.
	// Non-directory fds only accept file-applicable rights (write,
	// read, truncate, ioctl) — directory rights like MAKE_* are EINVAL
	// there, so /dev/null gets the file subset.
	full := ^uint64(0) & handled
	fileFull := full & (unix.LANDLOCK_ACCESS_FS_WRITE_FILE | unix.LANDLOCK_ACCESS_FS_READ_FILE |
		unix.LANDLOCK_ACCESS_FS_TRUNCATE | unix.LANDLOCK_ACCESS_FS_IOCTL_DEV)
	if err := addRule("/", rightReadOnly); err != nil {
		return err
	}
	for _, dir := range writable {
		if err := addRule(dir, full); err != nil {
			return err
		}
	}
	if err := addRule("/dev/null", fileFull); err != nil {
		return err
	}

	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		return fmt.Errorf("PR_SET_NO_NEW_PRIVS: %w", err)
	}
	if _, _, errno := syscall.Syscall(unix.SYS_LANDLOCK_RESTRICT_SELF, fd, 0, 0); errno != 0 {
		return fmt.Errorf("landlock_restrict_self: %v", errno)
	}
	return nil
}

// Exec replaces this process with argv — the last thing __sandbox
// does, so the sandboxed command inherits the ruleset process-wide.
func Exec(argv []string) error {
	if len(argv) == 0 {
		return errors.New("exec: empty command")
	}
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return err
	}
	return syscall.Exec(path, argv, os.Environ())
}

// deathAttr makes a spawned child die with its parent — a sandboxed
// command must not outlive tilde.
func deathAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}
