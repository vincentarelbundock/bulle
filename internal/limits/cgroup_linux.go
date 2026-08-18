package limits

import (
	"crypto/rand"
	"encoding/hex"
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
	// Creating a child is only half of what a run needs: the controllers must
	// also be delegated to it. Enabling them writes cgroup.subtree_control,
	// which fails with EBUSY whenever the parent holds processes of its own —
	// the ordinary case inside a container. Probing only the mkdir would report
	// the limits as enforced and then silently run without them.
	if err := controllersDelegatable(parent, allCgroupControllers); err != nil {
		support.CgroupReason = err.Error()
		return support
	}
	support.Cgroup = true
	return support
}

// allCgroupControllers are the controllers behind the cgroup-backed limits.
// Detect probes for all of them rather than for the ones this run asked for,
// so support is a property of the machine and cannot differ between the
// report and the run.
var allCgroupControllers = []string{"memory", "pids", "cpu"}

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
	// Sweep first: a run whose tree outlived it leaves a cgroup behind, because
	// a non-empty one cannot be removed. Nothing else ever cleans these up.
	sweepEmptyCgroups(parent)
	// The name carries a random component, not just the pid. Pids are recycled,
	// and adopting a same-named cgroup left by an earlier run would write this
	// run's limits onto a directory that may still hold that run's survivors —
	// which a timeout would then cgroup.kill.
	name, err := uniqueCgroupName(os.Getpid())
	if err != nil {
		return nil, err
	}
	path := filepath.Join(parent, name)
	if err := os.Mkdir(path, 0o755); err != nil {
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

// CanKill verifies that this delegated cgroup exposes the atomic whole-tree
// kill operation. Merely creating a cgroup is insufficient for timeout
// containment on kernels predating cgroup.kill.
func (c *Cgroup) CanKill() error {
	if c == nil {
		return errors.New("nil cgroup")
	}
	path := filepath.Join(c.path, "cgroup.kill")
	if err := syscall.Access(path, 0o2); err != nil {
		return fmt.Errorf("cgroup.kill is unavailable or not writable: %w", err)
	}
	return nil
}

// Close removes the cgroup. It is only removable once empty, so a failure here
// means something in the tree outlived the run; the directory is left behind
// rather than reported as a run failure, and the next Create sweeps it once it
// has emptied.
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

// uniqueCgroupName names this run's cgroup: the pid keeps it recognizable in
// /sys/fs/cgroup, the random suffix keeps it unique across pid recycling.
func uniqueCgroupName(pid int) (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("naming the run cgroup: %w", err)
	}
	return runCgroupName(pid) + "-" + hex.EncodeToString(buf), nil
}

// sweepEmptyCgroups removes bulle cgroups left by earlier runs. rmdir on a
// cgroup succeeds only when it holds no processes, so this can never disturb a
// run still in progress — including a concurrent one.
func sweepEmptyCgroups(parent string) {
	entries, err := os.ReadDir(parent)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), "bulle-") {
			_ = os.Remove(filepath.Join(parent, entry.Name()))
		}
	}
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
	missing, err := missingControllers(parent, controllers)
	if err != nil {
		return err
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

// missingControllers lists the "+name" writes needed to delegate controllers
// the parent has not already enabled for its children.
func missingControllers(parent string, controllers []string) ([]string, error) {
	enabled, err := readSet(filepath.Join(parent, "cgroup.subtree_control"))
	if err != nil {
		return nil, err
	}
	available, err := readSet(filepath.Join(parent, "cgroup.controllers"))
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, controller := range controllers {
		if enabled[controller] {
			continue
		}
		if !available[controller] {
			return nil, fmt.Errorf("the %s cgroup controller is not delegated to %s", controller, parent)
		}
		missing = append(missing, "+"+controller)
	}
	return missing, nil
}

// controllersDelegatable reports whether the controllers are already enabled
// for the parent's children or could still be enabled. The kernel refuses to
// write cgroup.subtree_control while the parent holds processes of its own (the
// "no internal process" rule), so a parent that is not empty can never gain a
// controller it lacks.
func controllersDelegatable(parent string, controllers []string) error {
	missing, err := missingControllers(parent, controllers)
	if err != nil {
		return err
	}
	if len(missing) == 0 {
		return nil
	}
	procs, err := os.ReadFile(filepath.Join(parent, "cgroup.procs"))
	if err != nil {
		return fmt.Errorf("cannot tell whether %s can delegate controllers", parent)
	}
	if len(strings.Fields(string(procs))) > 0 {
		return fmt.Errorf("%s holds processes of its own, so the %s controller(s) cannot be delegated to a child",
			parent, strings.Join(strings.Fields(strings.ReplaceAll(strings.Join(missing, " "), "+", "")), ", "))
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
