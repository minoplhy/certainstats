package compress

import (
	"strconv"
	"strings"
)

func AcceptsEncoding(header, name string) bool {
	wildcard := -1.0
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		q := 1.0
		for _, f := range fields[1:] {
			f = strings.TrimSpace(f)
			if strings.HasPrefix(f, "q=") {
				v, e := strconv.ParseFloat(strings.TrimPrefix(f, "q="), 64)
				if e != nil {
					q = 0
				} else {
					q = v
				}
			}
		}
		if fields[0] == name {
			return q > 0 && q <= 1
		}
		if fields[0] == "*" {
			wildcard = q
		}
	}
	return wildcard > 0 && wildcard <= 1
}
