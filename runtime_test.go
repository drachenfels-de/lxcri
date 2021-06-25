package lxcri

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lxc/lxcri/pkg/log"
	"github.com/lxc/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

func removeAll(t *testing.T, filename string) {
	err := os.RemoveAll(filename)
	require.NoError(t, err)
}

func newRuntime(t *testing.T) *Runtime {
	rt := NewRuntime(os.Getuid() != 0)
	rt.LogConfig.LogContext = map[string]string{
		"test": t.Name(),
	}
	rt.LogConfig.LogConsole = true
	if val, ok := os.LookupEnv("LXCRI_LIBEXEC"); ok {
		rt.LibexecDir = val
	}

	require.NoError(t, rt.Init())
	return rt
}

// NOTE a container that was created successfully must always be
// deleted, otherwise the go test runner will hang because it waits
// for the container process to exit.
func newConfig(t *testing.T, cmd string, args ...string) *ContainerConfig {
	rootfs, err := os.MkdirTemp("", "lxcri-test")
	require.NoError(t, err)
	err = unix.Chmod(rootfs, 0711)
	require.NoError(t, err)
	t.Logf("container rootfs: %s", rootfs)

	cmd = filepath.Join("/tmp", cmd)
	cmdAbs, err := filepath.Abs(cmd)
	require.NoError(t, err)
	cmdDest := "/" + filepath.Base(cmdAbs)

	spec := specki.NewSpec(rootfs, cmdDest)
	id := filepath.Base(rootfs)
	clog := log.ConsoleLogger(true, log.DebugLevel).Str("test", t.Name()).Str("cid", id).Logger()
	cfg := ContainerConfig{
		ContainerID: id, Spec: spec,
		Log:      clog,
		LogFile:  "/dev/stderr",
		LogLevel: "debug",
	}
	cfg.Spec.Linux.CgroupsPath = id + ".slice" // use /proc/self/cgroup"

	//
	cfg.Spec.Mounts = append(cfg.Spec.Mounts,
		specki.BindMount(cmdAbs, cmdDest),
	)
	return &cfg
}

func TestEmptyNamespaces(t *testing.T) {
	t.Parallel()
	rt := newRuntime(t)

	cfg := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg.Spec.Root.Path)

	// Clearing all namespaces should not work,
	// since the mount namespace must never be shared with the host.
	cfg.Spec.Linux.Namespaces = cfg.Spec.Linux.Namespaces[0:0]

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.Error(t, err)
	t.Logf("create error: %s", err)
	require.Nil(t, c)
}

func TestSharedPIDNamespace(t *testing.T) {
	t.Parallel()
	if os.Getuid() != 0 {
		t.Skipf("PID namespace sharing is only permitted as root.")
	}
	rt := newRuntime(t)

	cfg := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg.Spec.Root.Path)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	pidns := specs.LinuxNamespace{
		Type: specs.PIDNamespace,
		Path: fmt.Sprintf("/proc/%d/ns/pid", os.Getpid()),
	}

	for i, ns := range cfg.Spec.Linux.Namespaces {
		if ns.Type == specs.PIDNamespace {
			cfg.Spec.Linux.Namespaces[i] = pidns
		}
	}

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	err = c.Release()
	require.NoError(t, err)

	err = rt.Delete(ctx, c.ContainerID, true)
	require.NoError(t, err)
}

// TODO test uts namespace (shared with host)

// NOTE  works only if cgroup root is writable
// sudo chown -R $(whoami):$(whoami) /sys/fs/cgroup/$(cat /proc/self/cgroup  | grep '^0:' | cut -d: -f3)
func TestNonEmptyCgroup(t *testing.T) {
	t.Parallel()
	rt := newRuntime(t)

	cfg := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg.Spec.Root.Path)

	if os.Getuid() != 0 {
		err := unix.Chmod(cfg.Spec.Root.Path, 0777)
		require.NoError(t, err)

		err = unix.Chmod(rt.Root, 0755)
		require.NoError(t, err)

		cfg.Spec.Linux.UIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}
		cfg.Spec.Linux.GIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	//t.Logf("sleeping for a minute")
	//time.Sleep(60*time.Second)

	cfg2 := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg2.Spec.Root.Path)

	cfg2.Spec.Linux.CgroupsPath = cfg.Spec.Linux.CgroupsPath

	if os.Getuid() != 0 {
		cfg2.Spec.Linux.UIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}
		cfg2.Spec.Linux.GIDMappings = []specs.LinuxIDMapping{
			specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
		}
	}

	c2, err := rt.Create(ctx, cfg2)
	require.Error(t, err)
	t.Logf("create error: %s", err)

	err = c.Release()
	require.NoError(t, err)

	err = rt.Delete(ctx, c.ContainerID, true)
	require.NoError(t, err)

	err = c2.Release()
	require.NoError(t, err)

	require.NotNil(t, c2)
	err = rt.Delete(ctx, c2.ContainerID, true)
	require.NoError(t, err)
}

func TestRuntimePrivileged(t *testing.T) {
	t.Parallel()
	if os.Getuid() != 0 {
		t.Skipf("This tests only runs as root")
	}

	rt := newRuntime(t)
	defer removeAll(t, rt.Root)

	cfg := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg.Spec.Root.Path)

	testRuntime(t, rt, cfg)
}

// The following tests require the following setup:

// sudo /bin/sh -c "echo '$(whoami):20000:65536' >> /etc/subuid"
// sudo /bin/sh -c "echo '$(whoami):20000:65536' >> /etc/subgid"
// sudo chown -R $(whoami):$(whoami) /sys/fs/cgroup/unified$(cat /proc/self/cgroup  | grep '^0:' | cut -d: -f3)
// sudo chown -R $(whoami):$(whoami) /sys/fs/cgroup$(cat /proc/self/cgroup  | grep '^0:' | cut -d: -f3)
//
func TestRuntimeUnprivileged(t *testing.T) {
	t.Parallel()
	if os.Getuid() == 0 {
		t.Skipf("This test only runs as non-root")
	}

	rt := newRuntime(t)

	cfg := newConfig(t, "lxcri-test")
	defer removeAll(t, cfg.Spec.Root.Path)

	// The container UID must have full access to the rootfs.
	// MkdirTemp sets directory permissions to 0700.
	// If we the container UID (0) / or GID are not mapped to the owner (creator) of the rootfs,
	// then the rootfs and runtime directory permissions must be expanded.

	err := unix.Chmod(cfg.Spec.Root.Path, 0777)
	require.NoError(t, err)

	err = unix.Chmod(rt.Root, 0755)
	require.NoError(t, err)

	cfg.Spec.Linux.UIDMappings = []specs.LinuxIDMapping{
		specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
	}
	cfg.Spec.Linux.GIDMappings = []specs.LinuxIDMapping{
		specs.LinuxIDMapping{ContainerID: 0, HostID: 20000, Size: 65536},
	}

	testRuntime(t, rt, cfg)
}

func testRuntime(t *testing.T, rt *Runtime, cfg *ContainerConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*3)
	defer cancel()

	c, err := rt.Create(ctx, cfg)
	require.NoError(t, err)
	require.NotNil(t, c)

	state, err := c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateCreated, state.SpecState.Status)

	err = rt.Start(ctx, c)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.SpecState.Status)

	err = rt.Kill(ctx, c, unix.SIGUSR1)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateRunning, state.SpecState.Status)

	err = rt.Delete(ctx, c.ContainerID, true)
	require.NoError(t, err)

	state, err = c.State()
	require.NoError(t, err)
	require.Equal(t, specs.StateStopped, state.SpecState.Status)

	err = c.Release()
	require.NoError(t, err)
}
