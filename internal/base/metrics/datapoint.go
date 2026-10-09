package metrics

import (
	"encoding/json"
	"math"
)

type DataPoint [2]float64

func (p DataPoint) MarshalJSON() ([]byte, error) {
	if math.IsNaN(p[1]) || math.IsInf(p[1], 0) {
		return json.Marshal([2]any{p[0], nil})
	}
	return json.Marshal([2]float64(p))
}
