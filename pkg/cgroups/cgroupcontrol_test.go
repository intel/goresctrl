package cgroups

import (
	"io"
	"syscall"
	"testing"

	"github.com/intel/goresctrl/pkg/testutils"
)

var cpuacctMyGroupTasks string = ""
var testfiles fsiIface = NewFsiMock(map[string]mockFile{
	"/sys/fs/cgroup/blkio/kubepods/tasks": {
		data: []byte("1\n23\n4567890\n"),
	},
	"/sys/fs/cgroup/cpu/open/permission/denied/cgroup.procs": {
		// simulate open permission denied
		open: func(string) (fileIface, error) {
			return nil, syscall.EACCES
		},
	},
	"/sys/fs/cgroup/cpuacct/store/all/writes/tasks": {
		// everything that is written can be read
		// (no overwrite / truncate)
		write: func(b []byte) (int, error) {
			cpuacctMyGroupTasks = cpuacctMyGroupTasks + string(b) + "\n"
			return len(b), nil
		},
		read: func(b []byte) (int, error) {
			if len(cpuacctMyGroupTasks) == 0 {
				return 0, io.EOF
			}
			bytes := len(cpuacctMyGroupTasks)
			copy(b, []byte(cpuacctMyGroupTasks))
			cpuacctMyGroupTasks = ""
			return bytes, nil
		},
	},
	"/sys/fs/cgroup/cpuset/read/io/error/tasks": {
		// every read causes I/O error
		read: func(b []byte) (int, error) {
			return 0, syscall.EIO
		},
	},
	"/sys/fs/cgroup/devices/write/io/error/cgroup.procs": {
		// every write causes I/O error
		write: func(b []byte) (int, error) {
			return 0, syscall.EIO
		},
	},
})

func TestGetTasks(t *testing.T) {
	fsi = testfiles
	tasks, err := Blkio.Group("kubepods").GetTasks()
	testutils.VerifyNoError(t, err)
	testutils.VerifyStringSlices(t, []string{"1", "23", "4567890"}, tasks)
}

func TestGetProcesses(t *testing.T) {
	fsi = testfiles
	_, err := Cpu.Group("open/permission/denied").GetProcesses()
	testutils.VerifyError(t, err, 1, []string{"permission denied"})
}

func TestAddTasks(t *testing.T) {
	fsi = testfiles
	if err := Cpuacct.Group("store/all/writes").AddTasks("0", "987654321"); !testutils.VerifyNoError(t, err) {
		return
	}
	if err := Cpuacct.Group("store/all/writes").AddTasks(); !testutils.VerifyNoError(t, err) {
		return
	}
	if err := Cpuacct.Group("store/all/writes").AddTasks("12"); !testutils.VerifyNoError(t, err) {
		return
	}
	tasks, err := Cpuacct.Group("store/all/writes").GetTasks()
	testutils.VerifyNoError(t, err)
	testutils.VerifyStringSlices(t, []string{"0", "987654321", "12"}, tasks)
}

func TestAddProcesses(t *testing.T) {
	fsi = testfiles
	err := Devices.Group("write/io/error").AddProcesses("1")
	testutils.VerifyError(t, err, 1, []string{"input/output error"})
	err = Freezer.Group("file/not/found").AddProcesses("1")
	testutils.VerifyError(t, err, 1, []string{"file not found"})
}

func TestAsGroup(t *testing.T) {
	memGroupIn := Memory.Group("my/memory")
	memGroupOut := AsGroup(string(memGroupIn))
	testutils.VerifyStrings(t, string(memGroupIn), string(memGroupOut))
}

func TestGroupToController(t *testing.T) {
	c := Hugetlb.Group("my/group").Controller()
	testutils.VerifyStrings(t, "hugetlb", c.String())
}

func TestRelPath(t *testing.T) {
	relPath := NetCls.RelPath()
	testutils.VerifyStrings(t, "net_cls", relPath)
}
