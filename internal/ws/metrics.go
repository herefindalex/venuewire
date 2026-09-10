package ws

import "time"

func metricTime(milliseconds int64) time.Time {
	if milliseconds <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(milliseconds).UTC()
}
