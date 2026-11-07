package drivers

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

// i2cBus is one I2C adapter. Production: /dev/i2c-N. Tests: a fake.
type i2cBus interface {
	// ReadFrom does one read transaction of len(p) bytes from addr.
	ReadFrom(addr uint16, p []byte) error
	Close() error
}

const (
	ioctlI2CTimeout = 0x0702 // argument in units of 10 ms
	ioctlI2CRdwr    = 0x0707
	i2cMRead        = 0x0001
	i2cTimeoutUnits = 5 // 50 ms adapter timeout
)

type i2cMsg struct {
	Addr, Flags, Len uint16
	_                uint16
	Buf              *byte
}

type i2cRdwrData struct {
	Msgs  *i2cMsg
	NMsgs uint32
}

type devI2C struct{ f *os.File }

func openI2C(n int) (i2cBus, error) {
	f, err := os.OpenFile(fmt.Sprintf("/dev/i2c-%d", n), os.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	// Best effort: the bounded wrapper in the driver is the real limit.
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), ioctlI2CTimeout, i2cTimeoutUnits)
	return &devI2C{f: f}, nil
}

func (d *devI2C) ReadFrom(addr uint16, p []byte) error {
	msg := i2cMsg{Addr: addr, Flags: i2cMRead, Len: uint16(len(p)), Buf: &p[0]}
	data := i2cRdwrData{Msgs: &msg, NMsgs: 1}
	return ioctl(d.f.Fd(), ioctlI2CRdwr, unsafe.Pointer(&data))
}

func (d *devI2C) Close() error { return d.f.Close() }
