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

## Configuration

Block I/O classes can be configured with a yaml file. Example:

```
Classes:

  # Define a blockio class "LowPrioThrottled".
  # Containers in this class will be throttled and handled as
  # low priority in the I/O scheduler.

  LowPrioThrottled:

    # Weight without a Devices list specifies the default
    # I/O scheduler weight for all devices
    # that are not explicitly mentioned in following items.
    # This will be written to cgroups(.bfq).weight.
    # Weights range from 10 to 1000, the default is 100.

    - Weight: 80

    # Set all parameters for all /dev/sd* and /dev/vd* block
    # devices.

    - Devices:
        - /dev/sd[a-z]
        - /dev/vd[a-z]
      ThrottleReadBps: 50M   # max read bytes per second
      ThrottleWriteBps: 10M  # max write bytes per second
      ThrottleReadIOPS: 10k  # max read io operations per second
      ThrottleWriteIOPS: 5k  # max write io operations per second
      Weight: 50             # I/O scheduler (cfq/bfq) weight for
                             # these devices will be written to
                             # cgroups(.bfq).weight_device

    # Set parameters particularly for SSD devices.
    # This configuration overrides above configurations for those
    # /dev/sd* and /dev/vd* devices whose disk id contains "SSD".

    - Devices:
        - /dev/disk/by-id/*SSD*
      ThrottleReadBps: 100M
      ThrottleWriteBps: 40M
      # Not mentioning Throttle*IOPS means no I/O operations
      # throttling on matching devices.
      Weight: 50

  # Define a blockio class "HighPrioFullSpeed".
  # There is no throttling on these containers, and
  # they will be prioritized by the I/O scheduler.

  HighPrioFullSpeed:
    - Weight: 400
```
