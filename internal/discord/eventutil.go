package discord

import (
	"context"
	"time"

	"github.com/zodakzach/fight-night-discord-bot/internal/sources"
)

// pickNextEvent fetches a window of events and applies the same selection logic
// as handleNextEvent: prefer today's earliest, else next future, else most recent
// within a short window. Returns name and start time when found.
func pickNextEvent(p sources.Provider, loc *time.Location) (string, time.Time, bool, error) {
	nowUTC := time.Now().UTC()
	nowLocal := time.Now().In(loc)

	start := nowUTC.AddDate(0, 0, -1).Format("20060102")
	end := nowUTC.AddDate(0, 0, 30).Format("20060102")

	events, err := p.FetchEventsRange(context.Background(), start, end)
	if err != nil {
		return "", time.Time{}, false, err
	}

	todayKey := nowLocal.Format("20060102")

	var todayName string
	var todayAt time.Time
	var futureName string
	var futureAt time.Time
	var recentName string
	var recentAt time.Time

	for _, e := range events {
		t, err := parseAPITime(e.Date)
		if err != nil {
			continue
		}
		evLocalKey := t.In(loc).Format("20060102")
		name := e.Name
		if name == "" {
			name = e.ShortName
		}
		if evLocalKey == todayKey {
			if todayAt.IsZero() || t.Before(todayAt) {
				todayAt, todayName = t, name
			}
			continue
		}
		if t.After(nowUTC) {
			if futureAt.IsZero() || t.Before(futureAt) {
				futureAt, futureName = t, name
			}
			continue
		}
		if recentAt.IsZero() || t.After(recentAt) {
			recentAt, recentName = t, name
		}
	}

	if !todayAt.IsZero() {
		return todayName, todayAt, true, nil
	}
	if !futureAt.IsZero() {
		return futureName, futureAt, true, nil
	}
	if !recentAt.IsZero() && nowUTC.Sub(recentAt) <= 12*time.Hour {
		return recentName, recentAt, true, nil
	}
	return "", time.Time{}, false, nil
}
