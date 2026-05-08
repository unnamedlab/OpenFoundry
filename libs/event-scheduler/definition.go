package eventscheduler

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	schedulingcron "github.com/openfoundry/openfoundry-go/libs/scheduling-cron"
)

// ScheduleDefinition is a single row of schedules.definitions in the
// shape the runner uses internally. Surfaced publicly so callers
// (admin tools, custom-endpoint code) can read/write rows without
// re-deriving the shape.
type ScheduleDefinition struct {
	ID              uuid.UUID       `json:"id"`
	Name            string          `json:"name"`
	CronExpr        string          `json:"cron_expr"`
	CronFlavor      string          `json:"cron_flavor"`
	TimeZone        string          `json:"time_zone"`
	Enabled         bool            `json:"enabled"`
	Topic           string          `json:"topic"`
	PayloadTemplate json.RawMessage `json:"payload_template"`
	NextRunAt       time.Time       `json:"next_run_at"`
	LastRunAt       *time.Time      `json:"last_run_at,omitempty"`
}

// tryFlavor maps the schedule's cron_flavor string onto the
// scheduling-cron flavour enum. Returns *SchedulerError on miss.
func (d *ScheduleDefinition) tryFlavor() (schedulingcron.CronFlavor, error) {
	switch d.CronFlavor {
	case "unix5":
		return schedulingcron.Unix5, nil
	case "quartz6":
		return schedulingcron.Quartz6, nil
	}
	return 0, &SchedulerError{
		Kind:   ErrUnknownFlavor,
		Name:   d.Name,
		Flavor: d.CronFlavor,
	}
}

// tryTZ resolves the schedule's IANA time-zone name via
// time.LoadLocation. Returns *SchedulerError on miss.
func (d *ScheduleDefinition) tryTZ() (*time.Location, error) {
	loc, err := time.LoadLocation(d.TimeZone)
	if err != nil {
		return nil, &SchedulerError{
			Kind:     ErrInvalidTimeZone,
			Name:     d.Name,
			TimeZone: d.TimeZone,
		}
	}
	return loc, nil
}
