package devalue

import "time"

func epochMillis(ms int64) time.Time {
	return time.UnixMilli(ms).UTC()
}
