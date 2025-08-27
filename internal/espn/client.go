package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const ufcEventsURL = "https://site.api.espn.com/apis/site/v2/sports/mma/ufc/scoreboard?dates=%s"

type Event struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	ShortName string `json:"shortName"`
	Date      string `json:"date"` // RFC3339
}

type scoreboard struct {
	Events []Event `json:"events"`
}

type Client interface {
	FetchUFCEvents(ctx context.Context, yyyymmdd string) ([]Event, error)
	FetchUFCEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]Event, error)
}

type HTTPClient struct {
	HTTP      *http.Client
	UserAgent string
}

func NewClient(httpc *http.Client, userAgent string) *HTTPClient {
	if httpc == nil {
		httpc = http.DefaultClient
	}
	return &HTTPClient{HTTP: httpc, UserAgent: userAgent}
}

func (c *HTTPClient) FetchUFCEvents(ctx context.Context, yyyymmdd string) ([]Event, error) {
	// ESPN scoreboard 'dates' param accepts a year (e.g., 2025). Parse the year and fetch.
	if len(yyyymmdd) < 4 {
		return nil, fmt.Errorf("invalid date %q", yyyymmdd)
	}
	year := yyyymmdd[:4]
	return c.fetchByDates(ctx, year)
}

func (c *HTTPClient) FetchUFCEventsRange(ctx context.Context, startYYYYMMDD, endYYYYMMDD string) ([]Event, error) {
	// Fetch per-year covering the range and merge results.
	if len(startYYYYMMDD) < 4 || len(endYYYYMMDD) < 4 {
		return nil, fmt.Errorf("invalid range %q-%q", startYYYYMMDD, endYYYYMMDD)
	}
	startYear := startYYYYMMDD[:4]
	endYear := endYYYYMMDD[:4]
	if startYear == endYear {
		return c.fetchByDates(ctx, startYear)
	}
	var out []Event
	// Fetch start year
	evs, err := c.fetchByDates(ctx, startYear)
	if err != nil {
		return nil, err
	}
	out = append(out, evs...)
	// Fetch end year (only two years expected for 30-day lookahead near year end)
	evs, err = c.fetchByDates(ctx, endYear)
	if err != nil {
		return nil, err
	}
	out = append(out, evs...)
	return out, nil
}

func (c *HTTPClient) fetchByDates(ctx context.Context, datesParam string) ([]Event, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(ufcEventsURL, datesParam), nil)
	if err != nil {
		return nil, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("ESPN %d: %s", resp.StatusCode, string(body))
	}
	var sb scoreboard
	if err := json.NewDecoder(resp.Body).Decode(&sb); err != nil {
		return nil, err
	}
	return sb.Events, nil
}
