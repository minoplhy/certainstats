package web

/*
func CleanLinuxVersion(d *agentdata.Details) string {
	if d.LinuxVersionLen > 0 && int(d.LinuxVersionLen) <= len(d.LinuxVersion) {
		return string(d.LinuxVersion[:d.LinuxVersionLen])
	}
	// Fallback: trim null bytes if length wasn't provided accurately
	return string(bytes.TrimRight(d.LinuxVersion[:], "\x00"))
}

func CleanCpuModel(d *agentdata.Details) string {
	if d.CpuModelLen > 0 && int(d.CpuModelLen) <= len(d.CpuModel) {
		return string(d.CpuModel[:d.CpuModelLen])
	}
	// Fallback: trim null bytes if length wasn't provided accurately
	return string(bytes.TrimRight(d.CpuModel[:], "\x00"))
}
*/
