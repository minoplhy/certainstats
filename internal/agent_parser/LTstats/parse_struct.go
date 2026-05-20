package ltstats

const (
	NetHeaderSize = 35
	DetailsSize   = 112
	StatsTSize    = 40
)

type NetHeader struct {
	Token      [33]byte // 32 bytes token + null terminator
	Flags      uint8    // Version (7-byte) + IncludesDetails (1-byte)
	StatsCount uint8
}

func (h *NetHeader) Version() uint8 {
	return h.Flags & 0x7F
}

func (h *NetHeader) IncludesDetails() bool {
	return (h.Flags>>7)&0x01 == 1
}

type Details struct {
	Uptime          uint32
	LinuxVersion    [32]byte
	CpuModel        [48]byte
	LinuxVersionLen uint8
	CpuModelLen     uint8
	CpuCores        uint16
	RamSize         uint64
	SwapSize        uint64
	DiskSize        uint64
}

func MergeDecimal(before, after uint8) float64 {
	return float64(before) + float64(after)/100.0
}

type StatsT struct {
	Time               uint32
	CpuUsageBeforeDec  uint8
	CpuUsageAfterDec   uint8
	CpuIoWaitBeforeDec uint8
	CpuIoWaitAfterDec  uint8
	CpuStealBeforeDec  uint8
	CpuStealAfterDec   uint8
	RamUsageBeforeDec  uint8
	RamUsageAfterDec   uint8
	SwapUsageBeforeDec uint8
	SwapUsageAfterDec  uint8
	DiskUsageBeforeDec uint8
	DiskUsageAfterDec  uint8
	RxBytes            uint64
	TxBytes            uint64
	ReadSectors        uint64
	WrittenSectors     uint64
}
