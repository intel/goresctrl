# Intel RDT (Resource Director Technology)

## Background

Intel® RDT provides capabilities for cache and memory allocation and
monitoring. In Linux system the functionality is exposed to the user space via
the [resctrl](https://www.kernel.org/doc/Documentation/x86/intel_rdt_ui.txt)
filesystem. Cache and memory allocation in RDT is handled by using resource
control groups or classes of service (CLOSes). Resource allocation is specified
on the group level and each task (process/thread) is assigned to one group. In
the context of goresctrl the term 'RDT class' is used instead of 'resource
control group' or 'CLOS'.

Goresctrl supports all available RDT technologies, i.e. L2 and L3 Cache
Allocation (CAT) with Code and Data Prioritization (CDP) and Memory Bandwidth
Allocation (MBA) plus Cache Monitoring (CMT) and Memory Bandwidth Monitoring
(MBM).

## API

The API is described in
[pkg.go.dev](https://pkg.go.dev/github.com/intel/goresctrl/pkg/rdt).

# Configuration

## RDT Classes

Goresctrl provides hiearachical approach for managing RDT resources. The RDT
configuration is a two-level hierarchy consisting of partitions and classes: a
set of partitions each having a set of classes.

### Partitions

A partition consists of available resources and classes that share the
resources. Resources include portions of caches (L2 and L3) and memory
bandwidth (MB). Cache partitioning is exclusive: cache portions of two
partitions are not allowed to overlap. However, by design of the underlying
technology, MB allocations are not exclusive. Thus, it is possible to assign
all partitions 100% of memory bandwidth, for example.

### Classes

Classes represent the actual RDT classes processes are assigned to. In
contrast to partitions, cache allocation between classes under a specific
partition may overlap (and they usually do).

Requirements for class specifications:

- Names of classes must be unique accross all partitions
- Total number of classes (CLOSes) supported by the underlying hardware must
  not be exceeded.
  - **NOTE:** resctrl root and possible groups managed outside goresctrl are also
  accounted against this limit.
- Reserved name `DEFAULT` or an empty string refer to the resctrl root

## Configuration format

```yaml
# Common options
options:
  l2:
    # Set to false if L2 CAT must be available (Default is true).
    optional: [true|false]
  l3:
    # Set to false if L3 CAT must be available (Default is true).
    optional: [true|false]
  mb:
    # Set to false if MBA must be available (Default is true).
    optional: [true|false]
partitions:
  <partition-name>:
    # L2 CAT configuration of the partition
    l2Allocation:
      <cache-ids>:
        # L2 allocation spec used when CDP is not enabled, or, if CDP is
        # enabled but separate code and data specs are not specified
        unified: <cat-allocation-spec>
        # L2 allocation spec for the code path when CDP is enabled (optional)
        code: <cat-allocation-spec>
        # L2 allocation spec for the data path when CDP is enabled (optional)
        data: <cat-allocation-spec>
    # L3 CAT configuration of the partition
    l3Allocation:
      <cache-ids>:
        # L3 allocation spec used when CDP is not enabled, or, if CDP is
        # enabled but separate code and data specs are not specified
        unified: <cat-allocation-spec>
        # L3 allocation spec for the code path when CDP is enabled (optional)
        code: <cat-allocation-spec>
        # L3 allocation spec for the data path when CDP is enabled (optional)
        data: <cat-allocation-spec>
    # MBA configuration of the partition
    mbAllocation:
      # MB allocation spec
      <cache-ids>: <mb-allocation-spec>
    classes:
      <class-name>:
        l2Allocation:
          <cache-ids>:
            # L2 allocation spec used when CDP is not enabled, or, if CDP is
            # enabled but separate code and data specs are not specified
            unified: <cat-allocation-spec>
            # L2 allocation spec for the code path when CDP is enabled (optional)
            code: <cat-allocation-spec>
            # L2 allocation spec for the data path when CDP is enabled (optional)
            data: <cat-allocation-spec>
        l3Allocation:
          <cache-ids>:
            # L3 allocation spec used when CDP is not enabled, or, if CDP is
            # enabled but separate code and data specs are not specified
            unified: <cat-allocation-spec>
            # L3 allocation spec for the code path when CDP is enabled (optional)
            code: <cat-allocation-spec>
            # L3 allocation spec for the data path when CDP is enabled (optional)
            data: <cat-allocation-spec>
        mbAllocation:
          # MB allocation spec of the class
          <cache-ids>: <mb-allocation-spec>

        # Settings for the Kubernetes helper functions. Have no effect on the resctrl
        # configuration and control interface.
        kubernetes:
          # Set to true to deny assigning to this class via container annotation
          denyContainerAnnotation: [true|false]
          # Set to true to deny assigning to this class via pod annotation
          denyPodAnnotation: [true|false]
```

| Field | Format | Example | Description |
| ----- | ------ | ------- | ----------- |
| `<partition-name>` | string | `exclusive` | Name of a higher level RDT partition.
| `<class-name>`     | string | `guaranteed` | Name of an RDT class, mapping to a directory in the resctrl fs. Reserved name `DEFAULT` or an empty string can be used to refer to the root class.
| `<cache-ids>`      | cpuset (string) | `0,2,4,8-11` | Set of cache ids. Special value 'all' denotes a default used for cache "all the reset".
| `<cat-allocation-spec>` | percentage (string) | `"60%"` | Cache allocation spec, may be specified as relative (percentage) or absolute (bitmask). An absolute bitmask must be contiguous.
                          | hex bitmask (string) | `"0xf0"` |
                          | bit numbers (string) | `"0-3"` |
| `<mb-allocation-spec>` | list of strings | `[50%, 1000MBps]` | Memory bandwidth allocation spec, separarate values for percentage and MBps based allocation. The *MBps* value is in effect when resctrl is mounted with `-o mba_MBps`.

## Short forms

The configuration accepts shortforms in order to allow easier and more readable
configuration of the common and simple use cases.

1. Separate unified/code/data specs can be omitted, when no separate CDP config
   is desired, i.e.

      ```
          <cache-ids>: <cat-allocation-spec>

      ```

   is equal to

      ```
          <cache-ids>:
            unified: <cat-allocation-spec>
      ```

1. `<cache-ids>` may be omitted if no cache id specific configuration (and no
   CDP config for CAT) is desired, i.e.

      ```
        l3Allocation: "60%"
        mbAllocation: ["50%"]
      ```

   is equal to

      ```
        l3Allocation:
          all:
            unified: "60%"
        mbAllocation:
          all: ["50%"]
      ```

## Examples

Below is a config snippet that would allocate (ca.) 60% of the L3 cache lines
exclusively to the guaranteed class. The remaining 40% L3 is for burstable and
besteffort, Besteffort getting only 50% of this. guaranteed class gets full
memory bandwidth whereas the other classes are throttled to 50%.

```yaml
options:
  l2:
    optional: true
  l3:
    optional: true
  mb:
    optional: true
partitions:
  exclusive:
    # Allocate 80% of all L2 cache IDs to the "exclusive" partition
    l2Allocation: "80%"
    # Allocate 60% of all L3 cache IDs to the "exclusive" partition
    l3Allocation: "60%"
    mbAllocation: ["100%"]
    classes:
      guaranteed:
        # Allocate all of the partitions cache lines and memory bandwidth to "guaranteed"
        l2Allocation: "100%"
        l3Allocation: "100%"
        # The class will get 100% by default
        #mbAllocation: ["100%"]
  shared:
    # Allocate 20% of L2 and 40% L3 cache IDs to the "shared" partition
    # These will NOT overlap with the cache lines allocated for "exclusive" partition
    l2Allocation: "20%"
    l3Allocation: "40%"
    mbAllocation: ["50%"]
    classes:
      burstable:
        # Allow "burstable" to use all cache lines of the "shared" partition
        l2Allocation: "100%"
        l3Allocation: "100%"
        # The class will get 100% by default
        #mbAllocation: ["100%"]
      besteffort:
        # Allow "besteffort" to use all L2 but only half of the L3 cache
        # lines of the "shared" partition.
        # These will overlap with those used by "burstable"
        l2Allocation: "100%"
        l3Allocation: "50%"
        # The class will get 100% by default
        #mbAllocation: ["100%"]
      DEFAULT:
        # Also configure the resctrl root that all processes in the system are
        # placed in by default
        l2Allocation: "50%"
        l3Allocation: "30%"
        # The class will get 100% by default
        #mbAllocation: ["100%"]
```

The configuration also supports far more fine-grained control, e.g. per
cache-ID configuration (i.e. different cache ids, or sockets, having different
allocation) and Code and Data Prioritization (CDP) allowing different cache
allocation for code and data paths.

```yaml
...
    partitions:
      exclusive:
        l3Allocation: "60%"
        mbAllocation: ["100%"]
        classes:
          # Automatically gets 100% of what was allocated for the partition
          guaranteed:
      shared:
        l3Allocation:
          # 'all' denotes the default and must be specified
          all: "40%"
          # Specific cache allocation for cache-ids 2 and 3
          2-3: "20%"
        mbAllocation: ["100%"]
        classes:
          burstable:
            l3Allocation:
              all:
                unified: "100%"
                code: "100%"
                data: "80%"
              mbAllocation:
                all: ["80%"]
                2-3: ["50%"]
...
...
```

In addition, if the hardware details are known, raw bitmasks or bit numbers
(`0x1f` or '0-4`) can be used instead of percentages in order to be able to
configure cache allocations exactly as required. The bits in this case
correspond to those in /sys/fs/resctrl/ bitmasks. You can also mix relative
(percentage) and absolute (bitmask) allocations. For cases where the resctrl
filesystem is mounted with `-o mba_MBps` Memory bandwidth must be specifed in
MBps.

```yaml
...
    partitions:
      exclusive:
        # Specify bitmask in bit numbers
        l3Allocation: "8-19"
        # MBps value takes effect when resctrl mount option mba_MBps is used
        mbAllocation: ["100%", "100000MBps"]
        classes:
          # Automatically gets 100% of what was allocated for the partition
          guaranteed:
      shared:
        # Explicit bitmask
        l3Allocation: "0xff"
        mbAllocation: ["50%", "2000MBps"]
        classes:
          # burstable gets 100% of what was allocated for the partition
          burstable:
          besteffort:
            l3Allocation: "50%"
            # besteffort gets 50% of the 50% (i.e. 25% of total) or 1000MBps
            mbAllocation: ["50%", "1000MBps"]
```

## Dynamic Configuration

RDT supports dynamic configuration i.e. the parameters of existing classes may
changed on-the-fly.
