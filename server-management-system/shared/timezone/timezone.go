package timezone

import "time"

// Name is the zone every service interprets a calendar day in. Monitoring
// stamps its day counters with it and Reporting bounds its snapshots by it, so
// the dashboard's "today" and the emailed report mean the same 24 hours.
const Name = "Asia/Ho_Chi_Minh"

// Load returns the configured location.
func Load() (*time.Location, error) {
	return time.LoadLocation(Name)
}
