package ltstats

import (
	"encoding/binary"
)

func NetHeaderExtract(data [NetHeaderSize]byte) (NetHeader, error) {
	var header NetHeader
	copy(header.Token[:], data[0:33])
	header.Flags = data[33]
	header.StatsCount = data[34]
	return header, nil
}

func DetailsExtract(data [DetailsSize]byte) (Details, error) {
	var d Details
	d.Uptime = binary.LittleEndian.Uint32(data[0:4])
	copy(d.LinuxVersion[:], data[4:36])
	copy(d.CpuModel[:], data[36:84])
	d.LinuxVersionLen = data[84]
	d.CpuModelLen = data[85]
	d.CpuCores = binary.LittleEndian.Uint16(data[86:88])
	d.RamSize = binary.LittleEndian.Uint64(data[88:96])
	d.SwapSize = binary.LittleEndian.Uint64(data[96:104])
	d.DiskSize = binary.LittleEndian.Uint64(data[104:112])
	return d, nil
}

func StatTExtract(byte_input [StatsTSize]byte) (StatsT, error) {
	var st StatsT
	st.Time = binary.LittleEndian.Uint32(byte_input[0:4])
	st.CpuUsageBeforeDec = byte_input[4]
	st.CpuUsageAfterDec = byte_input[5]
	st.CpuIoWaitBeforeDec = byte_input[6]
	st.CpuIoWaitAfterDec = byte_input[7]
	st.CpuStealBeforeDec = byte_input[8]
	st.CpuStealAfterDec = byte_input[9]
	st.RamUsageBeforeDec = byte_input[10]
	st.RamUsageAfterDec = byte_input[11]
	st.SwapUsageBeforeDec = byte_input[12]
	st.SwapUsageAfterDec = byte_input[13]
	st.DiskUsageBeforeDec = byte_input[14]
	st.DiskUsageAfterDec = byte_input[15]

	st.RxBytes = uint64(byte_input[16]) |
		uint64(byte_input[17])<<8 |
		uint64(byte_input[18])<<16 |
		uint64(byte_input[19])<<24 |
		uint64(byte_input[20])<<32 |
		uint64(byte_input[21])<<40

	st.TxBytes = uint64(byte_input[22]) |
		uint64(byte_input[23])<<8 |
		uint64(byte_input[24])<<16 |
		uint64(byte_input[25])<<24 |
		uint64(byte_input[26])<<32 |
		uint64(byte_input[27])<<40

	st.ReadSectors = uint64(byte_input[28]) |
		uint64(byte_input[29])<<8 |
		uint64(byte_input[30])<<16 |
		uint64(byte_input[31])<<24 |
		uint64(byte_input[32])<<32 |
		uint64(byte_input[33])<<40

	st.WrittenSectors = uint64(byte_input[34]) |
		uint64(byte_input[35])<<8 |
		uint64(byte_input[36])<<16 |
		uint64(byte_input[37])<<24 |
		uint64(byte_input[38])<<32 |
		uint64(byte_input[39])<<40

	return st, nil
}
