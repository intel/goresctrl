package cgroups

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/intel/goresctrl/pkg/testutils"
)

var fsMockUtFiles map[string]mockFile = map[string]mockFile{
	"/my/emptyfile": {},
	"/my/emptydir": {
		info: &mockFileInfo{mode: os.ModeDir},
	},
	"/my/dir/data0": {data: []byte("abc")},
	"/my/dir/data1": {data: []byte("xyz")},
}

func TestWalk(t *testing.T) {
	fs := NewFsiMock(fsMockUtFiles)
	foundNotInMyDir := []string{}
	err := fs.Walk("/", func(path string, info os.FileInfo, err error) error {
		if filepath.Base(path) == "dir" {
			return filepath.SkipDir
		}
		foundNotInMyDir = append(foundNotInMyDir, path)
		return nil
	})
	testutils.VerifyNoError(t, err)
	sort.Strings(foundNotInMyDir)
	testutils.VerifyStringSlices(t, []string{"/", "/my", "/my/emptydir", "/my/emptyfile"}, foundNotInMyDir)
}

func TestReadWrite(t *testing.T) {
	var info os.FileInfo
	fs := NewFsiMock(fsMockUtFiles)
	f, err := fs.OpenFile("/my/dir/data0", os.O_WRONLY, 0)
	testutils.VerifyNoError(t, err)
	_, err = f.Write([]byte{})
	testutils.VerifyNoError(t, err)
	_, err = f.Write([]byte("01"))
	testutils.VerifyNoError(t, err)
	info, err = fs.Lstat("/my/dir/data0")
	testutils.VerifyNoError(t, err)
	if info.Size() != 3 {
		t.Errorf("expected file size %d, got %d", 3, info.Size())
	}
	_, err = f.Write([]byte("23"))
	testutils.VerifyNoError(t, err)
	if info.Size() != 4 {
		t.Errorf("expected file size %d, got %d", 4, info.Size())
	}
	f.Close()
	f, err = fs.OpenFile("/my/dir/data0", os.O_RDONLY, 0)
	testutils.VerifyNoError(t, err)
	buf := make([]byte, 10)
	bytes, err := f.Read(buf)
	testutils.VerifyNoError(t, err)
	if bytes != 4 {
		t.Errorf("expected to read %d bytes, Read returned %d", 4, bytes)
	}
	testutils.VerifyStringSlices(t, []string{"0123"}, []string{string(buf[:bytes])})
}
