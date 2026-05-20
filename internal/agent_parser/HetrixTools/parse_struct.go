package hetrixtools

type JSONdata struct {
	Version   string `json:"version"`
	Token     string `json:"SID"` // We treat `SID` as token, this is probably by design :D
	Agent     string `json:"agent"`
	User      string `json:"user"`
	OS        string `json:"os"`
	Kernel    string `json:"kernel"`
	Hostname  string `json:"hostname"`
	Time      string `json:"time"`
	ReqReboot string `json:"reqreboot"`
	Uptime    string `json:"uptime"`

	CPUModel   string `json:"cpumodel"`
	CPUSockets string `json:"cpusockets"`
	CPUCores   string `json:"cpucores"`
	CPUThreads string `json:"cputhreads"`
	CPUSpeed   string `json:"cpuspeed"`

	CPU   string `json:"cpu"`
	CPUwa string `json:"wa"`
	CPUst string `json:"st"`
	CPUus string `json:"us"`
	CPUsy string `json:"sy"`

	Load1  string `json:"load1"`
	Load5  string `json:"load5"`
	Load15 string `json:"load15"`

	RAMSize     string `json:"ramsize"`
	RAM         string `json:"ram"`
	RAMSwapSize string `json:"ramswapsize"`
	RAMSwap     string `json:"ramswap"`
	RAMBuff     string `json:"rambuff"`
	RAMCache    string `json:"ramcache"`

	Disks  string `json:"disks"`
	Inodes string `json:"inodes"`
	IOPS   string `json:"iops"`
	RAID   string `json:"raid"`
	ZP     string `json:"zp"`
	DH     string `json:"dh"`

	NICS string `json:"nics"`
	IPv4 string `json:"ipv4"`
	IPv6 string `json:"ipv6"`
	Conn string `json:"conn"`

	Temp string `json:"temp"`
	Serv string `json:"serv"`
	Cust string `json:"cust"`

	OPing string `json:"oping"`
	RPS1  string `json:"rps1"`
	RPS2  string `json:"rps2"`
}
