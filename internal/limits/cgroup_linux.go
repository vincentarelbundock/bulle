package limits

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const cgroupRoot = "/sys/fs/cgroup"

// cpuPeriod is the cpu.max accounting window in microseconds. 100ms is the
// kernel default; keeping it means a percentage converts to a quota by simply
// scaling, and short bursts are not chopped up more finely than the scheduler
// already does.
const cpuPeriod = 100_000

// Cgroup is a per-run cgroup v2 directory. The sandboxed process is created
// directly inside it via clone3's cgroup argument, so there is no window in
// which it runs unconstrained, and no way for it to escape by forking before
// it is moved.
type Cgroup struct {
	path string
	dir  *os.File
}

// Detect reports what this machine can enforce, without leaving anything
// behind. It probes by actually creating and removing a cgroup, because the
// permission bits alone do not tell you whether the controllers you need are
// delegated.
func Detect(goos string) Support {
	support := Support{GOOS: goos}
	parent, err := delegatedParent()
	if err != nil {
		support.CgroupReason = err.Error()
		return support
	}
	probe := filepath.Join(parent, runCgroupName(os.Getpid())+".probe")
	if err := os.Mkdir(probe, 0o755); err != nil {
		support.CgroupReason = fmt.Sprintf("cannot create a cgroup under %s", parent)
		return support
	}
	_ = os.Remove(probe)
	support.Cgroup = true
	return support
}

// Create makes the run's cgroup and writes the requested limits into it. Only
// cgroup-backed limits are consulted; the rest are applied as rlimits in the
// sandboxed child.
func Create(l Limits) (*Cgroup, error) {
	parent, err := delegatedParent()
	if err != nil {
		return nil, err
	}
	if err := enableControllers(parent, requiredControllers(l)); err != nil {
		return nil, err
	}
	path := filepath.Join(parent, runCgroupName(os.Getpid()))
	if err := os.Mkdir(path, 0o755); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("create cgroup %s: %w", path, err)
	}
	cgroup := &Cgroup{path: path}
	if err := cgroup.write(l); err != nil {
		_ = cgroup.Close()
		return nil, err
	}
	dir, err := os.Open(path)
	if err != nil {
		_ = cgroup.Close()
		return nil, fmt.Errorf("open cgroup %s: %w", path, err)
	}
	cgroup.dir = dir
	return cgroup, nil
}

// FD returns the directory descriptor to hand to clone3 so the child is born
// inside the cgroup.
func (c *Cgroup) FD() int {
	if c == nil || c.dir == nil {
		return 0
	}
	return int(c.dir.Fd())
}

func (c *Cgroup) write(l Limits) error {
	if l.Memory > 0 {
		if err := c.writeFile("memory.max", strconv.FormatUint(l.Memory, 10)); err != nil {
			return err
		}
	}
	if l.NProc > 0 {
		if err := c.writeFile("pids.max", strconv.FormatUint(l.NProc, 10)); err != nil {
			return err
		}
	}
	if l.CPU > 0 {
		quota := l.CPU * cpuPeriod / 100
		if err := c.writeFile("cpu.max", fmt.Sprintf("%d %d", quota, cpuPeriod)); err != nil {
			return err
		}
	}
	return nil
}

func (c *Cgroup) writeFile(name string, value string) error {
	path := filepath.Join(c.path, name)
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		return fmt.Errorf("set %s: %w", name, err)
	}
	return nil
}

// Kill terminates every process in the cgroup atomically. Unlike signalling a
// process group this cannot miss a process that called setsid, and cannot hit
// an unrelated process that inherited a recycled group id.
func (c *Cgroup) Kill() error {
	if c == nil {
		return nil
	}
	return c.writeFile("cgroup.kill", "1")
}

// Close removes the cgroup. It is only removable once empty, so a failure here
// means something in the tree outlived the run; the directory is left for the
// next Create to reuse rather than reported as a run failure.
func (c *Cgroup) Close() error {
	if c == nil {
		return nil
	}
	if c.dir != nil {
		_ = c.dir.Close()
		c.dir = nil
	}
	return os.Remove(c.path)
}

func runCgroupName(pid int) string {
	return "bulle-" + strconv.Itoa(pid)
}

func requiredControllers(l Limits) []string {
	var controllers []string
	if l.Memory > 0 {
		controllers = append(controllers, "memory")
	}
	if l.NProc > 0 {
		controllers = append(controllers, "pids")
	}
	if l.CPU > 0 {
		controllers = append(controllers, "cpu")
	}
	return controllers
}

// delegatedParent finds a cgroup directory bulle may create children in.
//
// The systemd user manager is delegated (Delegate=yes on user@.service), so on
// an ordinary desktop or server login its directory is writable and already
// has the controllers enabled. Inside a container the whole tree is typically
// delegated instead, and the process's own cgroup is the right parent. Neither
// is guaranteed, so a failure here is a supported outcome, not an error to
// escalate: the caller reports the limits as unenforced.
func delegatedParent() (string, error) {
	candidates := []string{}
	if uid := os.Getuid(); uid >= 0 {
		candidates = append(candidates, filepath.Join(cgroupRoot,
			"user.slice", fmt.Sprintf("user-%d.slice", uid), fmt.Sprintf("user@%d.service", uid)))
	}
	if own, err := ownCgroupPath(); err == nil {
		candidates = append(candidates, own)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err != nil || !info.IsDir() {
			continue
		}
		if syscall.Access(candidate, 0o2) != nil {
			continue
		}
		return candidate, nil
	}
	if _, err := os.Stat(filepath.Join(cgroupRoot, "cgroup.controllers")); err != nil {
		return "", errors.New("cgroup v2 is not mounted at /sys/fs/cgroup")
	}
	return "", errors.New("no delegated cgroup is writable by this user")
}

// ownCgroupPath reads the process's own cgroup v2 path. The unified hierarchy
// is always the "0::" line.
func ownCgroupPath() (string, error) {
	data, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		rest, ok := strings.CutPrefix(line, "0::")
		if !ok {
			continue
		}
		return filepath.Join(cgroupRoot, filepath.Clean("/"+strings.TrimSpace(rest))), nil
	}
	return "", errors.New("no cgroup v2 membership in /proc/self/cgroup")
}

// enableControllers makes the needed controllers available to the parent's
// children. Controllers already enabled are skipped, which matters because
// writing to cgroup.subtree_control fails when the parent holds processes of
// its own, and that write is unnecessary in the common case where the systemd
// user manager has already enabled them.
func enableControllers(parent string, controllers []string) error {
	enabled, err := readSet(filepath.Join(parent, "cgroup.subtree_control"))
	if err != nil {
		return err
	}
	available, err := readSet(filepath.Join(parent, "cgroup.controllers"))
	if err != nil {
		return err
	}
	var missing []string
	for _, controller := range controllers {
		if enabled[controller] {
			continue
		}
		if !available[controller] {
			return fmt.Errorf("the %s cgroup controller is not delegated to %s", controller, parent)
		}
		missing = append(missing, "+"+controller)
	}
	if len(missing) == 0 {
		return nil
	}
	path := filepath.Join(parent, "cgroup.subtree_control")
	if err := os.WriteFile(path, []byte(strings.Join(missing, " ")), 0o644); err != nil {
		return fmt.Errorf("enable cgroup controllers %s: %w", strings.Join(missing, " "), err)
	}
	return nil
}

func readSet(path string) (map[string]bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, field := range strings.Fields(string(data)) {
		set[field] = true
	}
	return set, nil
}
