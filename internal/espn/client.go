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
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(ufcEventsURL, yyyymmdd), nil)
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
