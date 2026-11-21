// Package wire holds the Knot DNS collector type's messages, shared by the server
// and certainstats-collector. Every message type starts with Prefix.
//
// Counters are Knot's own cumulative values, sent as read; the server turns them
// into increases. Zone status is current state. dnstap arrives as one-minute
// buckets that the server acknowledges, so a bucket is resent until stored.
package wire

const (
	Prefix   = "kn_"
	TypeName = "knot"
	// Switch is the collector-host variable that turns the type on, COLLECT_<NAME> as for every type.
	Switch = "COLLECT_KNOT"

	MsgConfig = "kn_config"
	MsgStatus = "kn_status"
	MsgStats  = "kn_stats"
	MsgZones  = "kn_zones"
	MsgDnstap = "kn_dnstap"
	MsgAck    = "kn_ack"

	StatsSeconds  = 60
	ZonesSeconds  = 300
	DnstapSeconds = 60

	// MaxCounters caps the counters in one kn_stats frame.
	MaxCounters = 20000
	// MaxZones caps the zones in one kn_zones frame.
	MaxZones = 5000
	// MaxZoneStats is the most zones with per-zone counters a collector may send.
	MaxZoneStats = 1000
	// MaxTop caps name rows in one dnstap bucket.
	MaxTop = 5000
	// MaxClients caps the client subnets in one dnstap bucket from an older
	// collector (Clients rows).
	MaxClients = 200
	// MaxNets caps the networks in one dnstap bucket (Nets).
	MaxNets = 5000
	// MaxClientRows caps the subnet-by-name rows in one dnstap bucket, and
	// each of NetNames and NetCodes.
	MaxClientRows = 5000
	// MaxPending is how many unacknowledged dnstap minutes a collector keeps.
	MaxPending = 60

	// Other is the qname of the row that folds every name below the cut.
	Other = "(other)"
	// NoZone is the zone of names outside every zone Knot serves.
	NoZone = "(none)"
)

// Config is sent by the server right after hello.
type Config struct {
	StatsSeconds  int `cbor:"stats_seconds"`
	ZonesSeconds  int `cbor:"zones_seconds"`
	DnstapSeconds int `cbor:"dnstap_seconds"`
	// MaxZoneStats and MaxTop cap what the collector sends; the host's own caps apply when lower.
	MaxZoneStats int `cbor:"max_zone_stats"`
	MaxTop       int `cbor:"max_top"`
}

// Status reports whether Knot answers and which optional parts are on.
type Status struct {
	Reachable bool   `cbor:"reachable"`
	Error     string `cbor:"error,omitempty"`
	Version   string `cbor:"version,omitempty"`
	Socket    string `cbor:"socket,omitempty"`
	ZoneStats bool   `cbor:"zone_stats"`
	// ModStats is false when Knot answered but sent no mod-stats counters.
	ModStats bool         `cbor:"mod_stats"`
	Dnstap   *DnstapState `cbor:"dnstap,omitempty"`
}

// DnstapState describes the dnstap listener, when the host switched it on.
type DnstapState struct {
	Socket    string `cbor:"socket" json:"socket"`
	Listening bool   `cbor:"listening" json:"listening"`
	Connected bool   `cbor:"connected" json:"connected"`
	Error     string `cbor:"error,omitempty" json:"error,omitempty"`
	// LastFrameMs is when the last dnstap frame arrived; zero for never.
	LastFrameMs int64 `cbor:"last_frame_ms" json:"last_frame_ms"`
	// Dropped counts frames that could not be decoded since the collector started.
	Dropped uint64 `cbor:"dropped" json:"dropped"`
	Clients bool   `cbor:"clients" json:"clients"`
}

// Counter is one Knot counter: section and item name it (server.zone-count,
// mod-stats.response-code), ID splits it (NXDOMAIN), Zone is empty for server-wide.
type Counter struct {
	Section string `cbor:"s"`
	Item    string `cbor:"i"`
	ID      string `cbor:"id,omitempty"`
	Zone    string `cbor:"z,omitempty"`
	Value   uint64 `cbor:"v"`
}

// Stats is one reading of Knot's counters.
type Stats struct {
	AtMs     int64     `cbor:"at_ms"`
	Counters []Counter `cbor:"counters"`
	// ZonesDropped counts zones left out by the per-zone cap.
	ZonesDropped int `cbor:"zones_dropped,omitempty"`
}

// Zone is one zone's current state. Times are absolute; zero is not scheduled.
type Zone struct {
	Name        string `cbor:"name"`
	Role        string `cbor:"role"` // primary or secondary
	Serial      uint32 `cbor:"serial"`
	HasSerial   bool   `cbor:"has_serial"`
	RefreshAtMs int64  `cbor:"refresh_at_ms,omitempty"`
	ExpireAtMs  int64  `cbor:"expire_at_ms,omitempty"`
	ResignAtMs  int64  `cbor:"resign_at_ms,omitempty"`
	Frozen      bool   `cbor:"frozen,omitempty"`
	Error       string `cbor:"error,omitempty"`
}

// Zones is one reading of every zone's status.
type Zones struct {
	AtMs      int64  `cbor:"at_ms"`
	Zones     []Zone `cbor:"zones"`
	Truncated int    `cbor:"truncated,omitempty"`
}

// Total is an exact count for one zone, qtype and rcode within a minute.
type Total struct {
	Zone  string `cbor:"z"`
	QType string `cbor:"t"`
	RCode string `cbor:"r"`
	Count uint64 `cbor:"n"`
}

// Name is one name row: the top combinations, plus one Other row per zone.
type Name struct {
	Zone  string `cbor:"z"`
	QName string `cbor:"q"`
	QType string `cbor:"t"`
	RCode string `cbor:"r"`
	Count uint64 `cbor:"n"`
}

// QName is one name's count over every qtype and rcode within a minute; names
// below the cut fold into one Other row per zone.
type QName struct {
	Zone  string `cbor:"z"`
	QName string `cbor:"q"`
	Count uint64 `cbor:"n"`
}

// Cut is the bound rule's cut for counts sorted largest first: the count of
// the first entry past top, or 0 when none is past it. Entries with a count
// above the cut are kept; the rest, ties at the cut included, fold to Other,
// and the cut is how far a kept or dropped count can be off.
func Cut(sorted []uint64, top int) uint64 {
	if top < 0 || len(sorted) <= top {
		return 0
	}
	return sorted[top]
}

// Client is one client subnet's count for one zone, name, qtype and rcode, only
// when the host allows it. Names below the cap fold into one Other row per
// subnet, zone, qtype and rcode, so a subnet's rows add up to its traffic.
type Client struct {
	Subnet string `cbor:"s"`
	Zone   string `cbor:"z"`
	QName  string `cbor:"q"`
	QType  string `cbor:"t"`
	RCode  string `cbor:"r"`
	Count  uint64 `cbor:"n"`
}

// Net is one client network's count over everything it asked within a
// minute. Networks below the cut fold into one Other network.
type Net struct {
	Subnet string `cbor:"s"`
	Count  uint64 `cbor:"n"`
}

// NetName is one network's count for one zone and name. Rows below the cut
// fold into one row per zone with network and name Other.
type NetName struct {
	Subnet string `cbor:"s"`
	Zone   string `cbor:"z"`
	QName  string `cbor:"q"`
	Count  uint64 `cbor:"n"`
}

// NetCode is one network's count for one zone, qtype and rcode. Rows below
// the cut fold into one row per zone with network, qtype and rcode Other.
type NetCode struct {
	Subnet string `cbor:"s"`
	Zone   string `cbor:"z"`
	QType  string `cbor:"t"`
	RCode  string `cbor:"r"`
	Count  uint64 `cbor:"n"`
}

// Size is one response-size bucket: responses of at most UpTo bytes (0 for larger).
type Size struct {
	UpTo  int    `cbor:"u"`
	Count uint64 `cbor:"n"`
}

// Dnstap is one minute of dnstap traffic, or one part of it: a busy minute is
// split into parts with disjoint rows so each frame fits. The server replaces
// rows by key, so a resent part changes nothing.
type Dnstap struct {
	MinuteMs int64    `cbor:"minute_ms"`
	Part     int      `cbor:"part,omitempty"`
	Parts    int      `cbor:"parts,omitempty"`
	Totals   []Total  `cbor:"totals"`
	Names    []Name   `cbor:"names"`
	Clients  []Client `cbor:"clients,omitempty"`
	Sizes    []Size   `cbor:"sizes,omitempty"`
	// QueriesOnly is set when Knot logged queries but no responses, so rcodes are unknown.
	QueriesOnly bool `cbor:"queries_only,omitempty"`
	// QNames counts each name over every qtype and rcode, cut by name.
	QNames []QName `cbor:"qnames,omitempty"`
	// Bounded is set by collectors that cut with the bound rule (see Cut);
	// QNameErr and NameErr are then this minute's cuts for QNames and Names,
	// and ride in every part.
	Bounded  bool   `cbor:"bounded,omitempty"`
	QNameErr uint64 `cbor:"qname_err,omitempty"`
	NameErr  uint64 `cbor:"name_err,omitempty"`
	// Nets, NetNames and NetCodes replace Clients in bounded collectors:
	// per network, network by name, and network by qtype and rcode, each cut
	// with the bound rule. NetErr, NetNameErr and NetCodeErr are their cuts,
	// and ride in every part.
	Nets       []Net     `cbor:"nets,omitempty"`
	NetNames   []NetName `cbor:"net_names,omitempty"`
	NetCodes   []NetCode `cbor:"net_codes,omitempty"`
	NetErr     uint64    `cbor:"net_err,omitempty"`
	NetNameErr uint64    `cbor:"net_name_err,omitempty"`
	NetCodeErr uint64    `cbor:"net_code_err,omitempty"`
}

// Ack confirms a stored dnstap minute; Error asks the collector to resend later.
type Ack struct {
	MinuteMs int64  `cbor:"minute_ms"`
	Part     int    `cbor:"part,omitempty"`
	Error    string `cbor:"error,omitempty"`
}

// SizeBuckets are the response-size bucket bounds, in bytes.
var SizeBuckets = []int{64, 128, 256, 512, 1232, 4096}
