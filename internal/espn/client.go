package espn

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

const ufcEventsURL = "https://site.api.espn.com/apis/site/v2/sports/mma/ufc/scoreboard?dates=%s"

// ESPN Core API: list competitions (bouts) for a specific event id
const ufcCoreEventCompetitionsURL = "https://sports.core.api.espn.com/v2/sports/mma/leagues/ufc/events/%s/competitions"

type Event struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// StartDate and EndDate are derived from the event's primary competition
	// when available; falls back to the event's top-level date for start.
	StartDate string `json:"startDate"` // RFC3339
	EndDate   string `json:"endDate"`   // RFC3339 (may be empty)

	// Back-compat fields kept for existing callers/tests. Prefer StartDate.
	ShortName string `json:"shortName"`
	Date      string `json:"date"` // RFC3339 (original top-level date)

	// Extended: include competitions so callers can resolve full cards
	// directly from the scoreboard payload when present.
	Competitions []Competition `json:"competitions"`
	// Optional links provided by ESPN (when present in payload)
	Links []struct {
		Href      string   `json:"href"`
		Text      string   `json:"text"`
		ShortText string   `json:"shortText"`
		Rel       []string `json:"rel"`
	} `json:"links"`
	// Optional logos/images for the event
	Logos []struct {
		Href string `json:"href"`
	} `json:"logos"`
}

// Root represents the ESPN UFC scoreboard root document (subset).
// It contains both the leagues with calendars and any embedded events.
type Root struct {
	Leagues []League `json:"leagues"`
	Events  []Event  `json:"events"`
}

type League struct {
	Calendar []CalEntry `json:"calendar"`
}

type CalEntry struct {
	Label     string `json:"label"`
	StartDate string `json:"startDate"`
	EndDate   string `json:"endDate"`
	Event     struct {
		Ref string `json:"$ref"`
	} `json:"event"`
}

// Competition and supporting types model just what we need for the full card.
type Competition struct {
	ID          string       `json:"id"`
	Date        string       `json:"date"`
	StartDate   string       `json:"startDate"`
	EndDate     string       `json:"endDate"`
	Type        CompType     `json:"type"`
	Competitors []Competitor `json:"competitors"`
	Status      struct {
		Type struct {
			State string `json:"state"`
		} `json:"type"`
	} `json:"status"`
}

type CompType struct {
	ID           string `json:"id"`
	Abbreviation string `json:"abbreviation"`
}

type Competitor struct {
	Order   int      `json:"order"`
	Winner  bool     `json:"winner"`
	Athlete Athlete  `json:"athlete"`
	Records []Record `json:"records"`
}

type Athlete struct {
	FullName  string `json:"fullName"`
	Display   string `json:"displayName"`
	ShortName string `json:"shortName"`
}

type Record struct {
	Summary string `json:"summary"`
}

// Fight is a simplified view of a bout for output and downstream use.
type Fight struct {
	WeightClass string
	RedName     string
	RedRecord   string
	BlueName    string
	BlueRecord  string
	Winner      string
	Scheduled   time.Time
}

// Note: legacy date-range fetcher interface removed in favor of a TZ-aware
// selection entrypoint that resolves the full event and card.

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

// Removed legacy FetchUFCEvents/Range and internal fetchByDates; use
// FetchUFCScoreboardRoot to fetch a year's scoreboard when needed.

// Bout represents a single fight on a card.
type Bout struct {
	Fighter1    string
	Fighter2    string
	WeightClass string // e.g., "Lightweight"; may be empty
}

// FetchUFCCardForEvent retrieves the fight card for a given event ID.
// It only fetches data for that specific event to avoid heavy scraping.
// Note: ESPN's core API provides competitor references which require
// additional calls to resolve athlete display names. This method performs
// the minimal required fetches to build a simple bout list.
func (c *HTTPClient) FetchUFCCardForEvent(ctx context.Context, eventID string) ([]Bout, error) {
	if strings.TrimSpace(eventID) == "" {
		return nil, fmt.Errorf("eventID is required")
	}

	// Step 1: list competitions (individual fights) for the event
	listURL := fmt.Sprintf(ufcCoreEventCompetitionsURL, eventID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, listURL, nil)
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

	var compList struct {
		Items []struct {
			Ref string `json:"$ref"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&compList); err != nil {
		return nil, err
	}
	if len(compList.Items) == 0 {
		return nil, nil
	}

	// Helper to GET JSON into v
	doGet := func(url string, v any) error {
		r, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return err
		}
		if c.UserAgent != "" {
			r.Header.Set("User-Agent", c.UserAgent)
		}
		r.Header.Set("Accept", "application/json")
		rs, err := c.HTTP.Do(r)
		if err != nil {
			return err
		}
		defer rs.Body.Close()
		if rs.StatusCode/100 != 2 {
			body, _ := io.ReadAll(io.LimitReader(rs.Body, 1024))
			return fmt.Errorf("ESPN %d: %s", rs.StatusCode, string(body))
		}
		return json.NewDecoder(rs.Body).Decode(v)
	}

	// Step 2: fetch each competition and resolve athlete names
	bouts := make([]Bout, 0, len(compList.Items))
	for _, it := range compList.Items {
		var comp struct {
			Type struct {
				Text string `json:"text"`
			} `json:"type"`
			Competitors []struct {
				Athlete struct {
					Ref string `json:"$ref"`
				} `json:"athlete"`
			} `json:"competitors"`
		}
		if err := doGet(it.Ref, &comp); err != nil {
			return nil, err
		}
		names := make([]string, 0, 2)
		for _, cpt := range comp.Competitors {
			if cpt.Athlete.Ref == "" {
				continue
			}
			var ath struct {
				DisplayName string `json:"displayName"`
			}
			if err := doGet(cpt.Athlete.Ref, &ath); err != nil {
				return nil, err
			}
			if ath.DisplayName != "" {
				names = append(names, ath.DisplayName)
			}
		}
		// Ensure we always have two slots
		f1, f2 := "", ""
		if len(names) > 0 {
			f1 = names[0]
		}
		if len(names) > 1 {
			f2 = names[1]
		}
		bouts = append(bouts, Bout{Fighter1: f1, Fighter2: f2, WeightClass: comp.Type.Text})
	}
	return bouts, nil
}

// FetchNextOrOngoingEventAndCard fetches the UFC scoreboard root for the surrounding years,
// selects the ongoing event (if now ∈ [start,end) in UTC) or the next event (minimal start > now),
// resolves the full event (using embedded or fetched $ref), and returns the full card.
// It returns the event, fights, start/end in UTC, ok=false when not found, or an error.
func (c *HTTPClient) FetchNextOrOngoingEventAndCard(ctx context.Context, ignoreLabels []string, clock func() time.Time) (*Event, []Fight, time.Time, time.Time, bool, error) {
	nowUTC := clock().UTC()
	// Fetch scoreboard roots for prev, current, next year to cover boundaries
	years := []int{nowUTC.Year() - 1, nowUTC.Year(), nowUTC.Year() + 1}
	var combined Root
	for _, y := range years {
		root, err := c.FetchUFCScoreboardRoot(ctx, fmt.Sprintf("%d", y))
		if err != nil {
			return nil, nil, time.Time{}, time.Time{}, false, err
		}
		// Merge calendars into a single league
		if len(root.Leagues) > 0 {
			if len(combined.Leagues) == 0 {
				combined.Leagues = []League{{}}
			}
			combined.Leagues[0].Calendar = append(combined.Leagues[0].Calendar, root.Leagues[0].Calendar...)
		}
		combined.Events = append(combined.Events, root.Events...)
	}

	// Select calendar entry using UTC logic
	pick, stUTC, enUTC, selErr := findNextOrOngoingEventUTC(combined, ignoreLabels, clock)
	if selErr != nil {
		if selErr == errNoEventSelected {
			return nil, nil, time.Time{}, time.Time{}, false, nil
		}
		return nil, nil, time.Time{}, time.Time{}, false, selErr
	}

	// Resolve full event
	ev, err := resolveFullEvent(combined, pick, true, c.HTTP)
	if err != nil {
		return nil, nil, time.Time{}, time.Time{}, false, err
	}

	fights := listFullCard(ev, time.UTC)
	// Fallback: if no competitions present, try fetching via core API and adapt
	if len(fights) == 0 && ev != nil && ev.ID != "" {
		if bouts, err := c.FetchUFCCardForEvent(ctx, ev.ID); err == nil && len(bouts) > 0 {
			for _, b := range bouts {
				fights = append(fights, Fight{WeightClass: b.WeightClass, RedName: b.Fighter1, BlueName: b.Fighter2})
			}
		}
	}
	return ev, fights, stUTC, enUTC, true, nil
}

// FetchUFCScoreboardRoot fetches the UFC scoreboard document for a given ESPN 'dates'
// parameter (usually a year like "2025") and decodes into Root.
func (c *HTTPClient) FetchUFCScoreboardRoot(ctx context.Context, dates string) (Root, error) {
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf(ufcEventsURL, dates), nil)
	if err != nil {
		return Root{}, err
	}
	if c.UserAgent != "" {
		req.Header.Set("User-Agent", c.UserAgent)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Root{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return Root{}, fmt.Errorf("ESPN %d", resp.StatusCode)
	}
	var root Root
	if err := json.NewDecoder(resp.Body).Decode(&root); err != nil {
		return Root{}, err
	}
	return root, nil
}

// ---- Internal helpers (tz-aware selection, event resolution, card building) ----

var errNoEventSelected = fmt.Errorf("no matching calendar entry")

func parseISOUTC(s string) (time.Time, error) {
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04Z07:00",
		"2006-01-02T15Z07:00",
		"2006-01-02T15:04:05.999Z07:00",
		time.RFC3339Nano,
		"2006-01-02T15:04:05Z0700",
	}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), nil
		} else {
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("empty time")
	}
	return time.Time{}, lastErr
}

func containsAnyIgnore(label string, ignores []string) bool {
	if label == "" || len(ignores) == 0 {
		return false
	}
	l := strings.ToLower(label)
	for _, s := range ignores {
		if s == "" {
			continue
		}
		if strings.Contains(l, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func findNextOrOngoingEventUTC(root Root, ignoreLabels []string, clock func() time.Time) (*CalEntry, time.Time, time.Time, error) {
	nowUTC := clock().UTC()

	var ongoing *CalEntry
	var ongoingST, ongoingEN time.Time
	var next *CalEntry
	var nextST, nextEN time.Time

	for _, lg := range root.Leagues {
		for i := range lg.Calendar {
			ce := &lg.Calendar[i]
			if containsAnyIgnore(ce.Label, ignoreLabels) {
				continue
			}
			if strings.TrimSpace(ce.StartDate) == "" {
				continue
			}
			stUTC, err := parseISOUTC(ce.StartDate)
			if err != nil {
				continue
			}
			var enUTC time.Time
			if strings.TrimSpace(ce.EndDate) != "" {
				if t, err := parseISOUTC(ce.EndDate); err == nil {
					enUTC = t
				}
			}
			// ONGOING when end exists and now ∈ [start, end)
			if !enUTC.IsZero() && (nowUTC.Equal(stUTC) || (nowUTC.After(stUTC) && nowUTC.Before(enUTC))) {
				if ongoing == nil || stUTC.Before(ongoingST) {
					ongoing, ongoingST, ongoingEN = ce, stUTC, enUTC
				}
				continue
			}
			// NEXT when start > now
			if stUTC.After(nowUTC) {
				if next == nil || stUTC.Before(nextST) {
					next, nextST, nextEN = ce, stUTC, enUTC
				}
			}
		}
	}
	if ongoing != nil {
		return ongoing, ongoingST, ongoingEN, nil
	}
	if next != nil {
		return next, nextST, nextEN, nil
	}
	return nil, time.Time{}, time.Time{}, errNoEventSelected
}

var eventIDFromRefRe = regexp.MustCompile(`/events/(\d+)`)

func eventIDFromRef(ref string) (string, bool) {
	if ref == "" {
		return "", false
	}
	m := eventIDFromRefRe.FindStringSubmatch(ref)
	if len(m) == 2 {
		return m[1], true
	}
	return "", false
}

func similarName(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	al := strings.ToLower(a)
	bl := strings.ToLower(b)
	if al == bl {
		return true
	}
	return strings.Contains(al, bl) || strings.Contains(bl, al)
}

func resolveFullEvent(root Root, pick *CalEntry, allowFetch bool, httpClient *http.Client) (*Event, error) {
	if pick == nil {
		return nil, fmt.Errorf("nil calendar entry")
	}
	if id, ok := eventIDFromRef(pick.Event.Ref); ok {
		for i := range root.Events {
			if root.Events[i].ID == id {
				return &root.Events[i], nil
			}
		}
	}
	pickStart, _ := parseISOUTC(pick.StartDate)
	for i := range root.Events {
		ev := &root.Events[i]
		evT, err := parseISOUTC(ev.Date)
		if err != nil {
			continue
		}
		dt := evT.Sub(pickStart)
		if dt < 0 {
			dt = -dt
		}
		if dt > 48*time.Hour {
			continue
		}
		if similarName(ev.Name, pick.Label) || similarName(ev.ShortName, pick.Label) || similarName(pick.Label, ev.Name) || similarName(pick.Label, ev.ShortName) {
			return ev, nil
		}
	}
	if allowFetch && pick.Event.Ref != "" && httpClient != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, pick.Event.Ref, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return nil, fmt.Errorf("fetch event %q: status %d", pick.Event.Ref, resp.StatusCode)
		}
		var ev Event
		if err := json.NewDecoder(resp.Body).Decode(&ev); err != nil {
			return nil, err
		}
		return &ev, nil
	}
	return nil, fmt.Errorf("event not found in root and fetch disabled or missing $ref")
}

func listFullCard(ev *Event, loc *time.Location) []Fight {
	if ev == nil {
		return nil
	}
	fights := make([]Fight, 0, len(ev.Competitions))
	for _, c := range ev.Competitions {
		red, blue := extractNames(c.Competitors)
		redRec, blueRec := extractRecords(c.Competitors)
		winner := ""
		if strings.EqualFold(c.Status.Type.State, "post") {
			if w := winnerName(c.Competitors, red, blue); w != "" {
				winner = w
			}
		}
		sched := time.Time{}
		for _, ts := range []string{c.StartDate, c.Date} {
			if strings.TrimSpace(ts) == "" {
				continue
			}
			if t, err := parseISOUTC(ts); err == nil {
				sched = t.In(loc)
				break
			}
		}
		wc := c.Type.Abbreviation
		if wc == "" {
			wc = c.Type.ID
		}
		fights = append(fights, Fight{
			WeightClass: wc,
			RedName:     red,
			RedRecord:   redRec,
			BlueName:    blue,
			BlueRecord:  blueRec,
			Winner:      winner,
			Scheduled:   sched,
		})
	}
	return fights
}

func extractNames(cs []Competitor) (red, blue string) {
	var rOK, bOK bool
	for _, c := range cs {
		name := firstNonEmpty(c.Athlete.FullName, c.Athlete.Display, c.Athlete.ShortName)
		if c.Order == 1 && !rOK {
			red, rOK = name, true
		} else if c.Order == 2 && !bOK {
			blue, bOK = name, true
		}
	}
	if !rOK && len(cs) > 0 {
		red = firstNonEmpty(cs[0].Athlete.FullName, cs[0].Athlete.Display, cs[0].Athlete.ShortName)
	}
	if !bOK && len(cs) > 1 {
		blue = firstNonEmpty(cs[1].Athlete.FullName, cs[1].Athlete.Display, cs[1].Athlete.ShortName)
	}
	return
}

func extractRecords(cs []Competitor) (redRec, blueRec string) {
	for _, c := range cs {
		rec := ""
		if len(c.Records) > 0 {
			rec = c.Records[0].Summary
		}
		if c.Order == 1 && redRec == "" {
			redRec = rec
		} else if c.Order == 2 && blueRec == "" {
			blueRec = rec
		}
	}
	return
}

func winnerName(cs []Competitor, red, blue string) string {
	for _, c := range cs {
		if c.Winner {
			if c.Order == 1 {
				return red
			}
			if c.Order == 2 {
				return blue
			}
			return firstNonEmpty(c.Athlete.FullName, c.Athlete.Display, c.Athlete.ShortName)
		}
	}
	return ""
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
