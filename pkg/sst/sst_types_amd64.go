// Code generated by cmd/cgo -godefs; DO NOT EDIT.
// cgo -godefs -- -I/usr/src/linux/include/uapi -I/usr/src/linux/include _sst_types_amd64.go

package sst

const (
	ISST_IF_GET_PHY_ID   = 0xc008fe01
	ISST_IF_IO_CMD       = 0x4008fe02
	ISST_IF_MBOX_COMMAND = 0xc008fe03
)

type isstIfCPUMaps struct {
	Cmd_count uint32
	Cpu_map   [1]isstIfCPUMap
}
type isstIfCPUMap struct {
	Logical_cpu  uint32
	Physical_cpu uint32
}

type isstIfIoReg struct {
	Read_write  uint32
	Logical_cpu uint32
	Reg         uint32
	Value       uint32
}
type isstIfIoRegs struct {
	Req_count uint32
	Io_reg    [1]isstIfIoReg
}

type isstIfMboxCmd struct {
	Logical_cpu uint32
	Parameter   uint32
	Req_data    uint32
	Resp_data   uint32
	Command     uint16
	Sub_command uint16
	Reserved    uint32
}
type isstIfMboxCmds struct {
	Cmd_count uint32
	Mbox_cmd  [1]isstIfMboxCmd
}
