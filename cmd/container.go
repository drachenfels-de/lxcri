package main

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
)

// ContainerInfo holds the information about a single container.
// It is created at 'create' within the container runtime dir and not changed afterwards.
// It is removed when the container is deleted.
type ContainerInfo struct {
	ContainerID string
	CreatedAt   time.Time
	RuntimeRoot string

	BundlePath    string
	ConsoleSocket string `json;,omitempty`
	// PidFile is the absolute path to the PID file of the container monitor process (crio-lxc-start)
	PidFile          string
	MonitorCgroupDir string

	// values derived from spec
	CgroupDir string

	// feature gates
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool

	*specs.Spec
}

// RuntimePath returns the absolute path witin the container root
func (c ContainerInfo) RuntimePath(subPath ...string) string {
	return filepath.Join(c.RuntimeRoot, c.ContainerID, filepath.Join(subPath...))
}

func (c ContainerInfo) ConfigFilePath() string {
	return c.RuntimePath("config")
}

func (c ContainerInfo) Pid() (int, error) {
	// #nosec
	data, err := ioutil.ReadFile(c.PidFile)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(string(data))
	return strconv.Atoi(s)
}

func (c ContainerInfo) CreatePidFile(pid int) error {
	return createPidFile(c.PidFile, pid)
}

// RuntimeRoot and ContainerID must be set
func (c *ContainerInfo) Load() error {
	p := c.RuntimePath("container.json")
	if err := decodeFileJSON(c, p); err != nil {
		return err
	}
	return c.ReadSpec()
}

func (c *ContainerInfo) Create() error {
	p := c.RuntimePath("container.json")
	return encodeFileJSON(p, c, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
}

func(c *ContainerInfo) ReadSpec() error {
	return decodeFileJSON(c.Spec, filepath.Join(c.BundlePath, "config.json"))
}
