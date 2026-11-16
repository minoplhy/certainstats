package knot

import (
	cwire "certainstats/internal/collector/wire"
)

func encodeEnv(msgType string, body any) (cwire.Envelope, error) { return cwire.Encode(msgType, body) }
