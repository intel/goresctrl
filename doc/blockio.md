# Block I/O

## Background

The cgroup block I/O controller,
[blkio](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v1/blkio-controller.html)
in cgroup v1,
[io](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html#io)
in cgroup v2, in Linux kernel controls I/O scheduler weights and I/O
bandwidth per block device.

The blockio package in goresctrl is configured with class-based block
I/O controller parameters, where different parameters can be
configured for each class. The package provides two separate output
options: parameters of a class can be applied directly to cgroup v1
directory structure, or they can be exported as
[Linux BlockIO OCI spec](https://github.com/opencontainers/runtime-spec/blob/master/config-linux.md#block-io).

## API

The API is described in
[pkg.go.dev](https://pkg.go.dev/github.com/intel/goresctrl/pkg/blockio).
