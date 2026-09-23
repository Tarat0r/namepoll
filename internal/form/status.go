package form

import "time"

type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusActive    Status = "active"
	StatusFinished  Status = "finished"
)

func (d Definition) Status(now time.Time) Status {
	switch {
	case now.Before(d.StartDate):
		return StatusScheduled
	case now.After(d.EndDate):
		return StatusFinished
	default:
		return StatusActive
	}
}
