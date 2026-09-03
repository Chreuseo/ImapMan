package processor

import "time"

func timeTicker(interval time.Duration) *time.Ticker {
	return time.NewTicker(interval)
}
