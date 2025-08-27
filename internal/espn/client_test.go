package espn

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// rewriteTransport redirects all requests to a given base URL, preserving the query.
type rewriteTransport struct{ base *url.URL }

func (rt *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u := *rt.base
	u.RawQuery = req.URL.RawQuery
	// Keep whatever path is requested, but our test servers generally ignore it.
	u.Path = req.URL.Path
	req2 := req.Clone(req.Context())
	req2.URL = &u
	req2.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(req2)
}

func TestFetchUFCEvents_UsesYearAndHeaders(t *testing.T) {
	var mu sync.Mutex
	var gotDates []string
	var gotUA []string
	var gotAccept []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotDates = append(gotDates, r.URL.Query().Get("dates"))
		gotUA = append(gotUA, r.Header.Get("User-Agent"))
		gotAccept = append(gotAccept, r.Header.Get("Accept"))
		json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]string{{
				"id": "1", "name": "UFC Test", "shortName": "UT", "date": "2025-01-15T20:00:00Z",
			}},
		})
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	httpc := &http.Client{Transport: &rewriteTransport{base: base}}
	c := NewClient(httpc, "test-agent")

	evs, err := c.FetchUFCEvents(context.Background(), "20250115")
	if err != nil {
		t.Fatalf("FetchUFCEvents error: %v", err)
	}
	if len(evs) != 1 || evs[0].ID != "1" || evs[0].Name != "UFC Test" || evs[0].ShortName != "UT" {
		t.Fatalf("unexpected events: %+v", evs)
	}

	mu.Lock()
	defer mu.Unlock()
	if !reflect.DeepEqual(gotDates, []string{"2025"}) {
		t.Fatalf("expected single year query 2025, got %v", gotDates)
	}
	if gotUA[0] != "test-agent" {
		t.Fatalf("expected user-agent set, got %q", gotUA[0])
	}
	if gotAccept[0] != "application/json" {
		t.Fatalf("expected Accept header application/json, got %q", gotAccept[0])
	}
}

func TestFetchUFCEventsRange_SameYearAndCrossYear(t *testing.T) {
	var mu sync.Mutex
	var gotDates []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		gotDates = append(gotDates, r.URL.Query().Get("dates"))
		// return different IDs per year so we can ensure both merged
		year := r.URL.Query().Get("dates")
		id := "y" + year
		json.NewEncoder(w).Encode(map[string]any{
			"events": []map[string]string{{
				"id": id, "name": "E" + year, "shortName": "S" + year, "date": "2024-12-31T19:00:00Z",
			}},
		})
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	httpc := &http.Client{Transport: &rewriteTransport{base: base}}
	c := NewClient(httpc, "ua")

	// Same year
	gotDates = nil
	evs, err := c.FetchUFCEventsRange(context.Background(), "20250101", "20250131")
	if err != nil {
		t.Fatalf("same-year range error: %v", err)
	}
	if len(evs) != 1 || !strings.HasPrefix(evs[0].ID, "y2025") {
		t.Fatalf("expected events from 2025, got %+v", evs)
	}
	mu.Lock()
	if !reflect.DeepEqual(gotDates, []string{"2025"}) {
		mu.Unlock()
		t.Fatalf("expected single fetch for 2025, got %v", gotDates)
	}
	mu.Unlock()

	// Cross year
	gotDates = nil
	evs, err = c.FetchUFCEventsRange(context.Background(), "20241220", "20250110")
	if err != nil {
		t.Fatalf("cross-year range error: %v", err)
	}
	if len(evs) != 2 {
		t.Fatalf("expected two merged events, got %d", len(evs))
	}
	mu.Lock()
	if !reflect.DeepEqual(gotDates, []string{"2024", "2025"}) {
		mu.Unlock()
		t.Fatalf("expected fetches for 2024 then 2025, got %v", gotDates)
	}
	mu.Unlock()
}

func TestFetchErrors_InvalidInputAndHTTPAndJSON(t *testing.T) {
	// invalid input
	c := NewClient(http.DefaultClient, "ua")
	if _, err := c.FetchUFCEvents(context.Background(), "20"); err == nil {
		t.Fatalf("expected error for short date")
	}
	if _, err := c.FetchUFCEventsRange(context.Background(), "20", "20250101"); err == nil {
		t.Fatalf("expected error for invalid range start")
	}

	// non-2xx
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server exploded"))
	}))
	defer srvErr.Close()
	baseErr, _ := url.Parse(srvErr.URL)
	httpcErr := &http.Client{Transport: &rewriteTransport{base: baseErr}}
	cErr := NewClient(httpcErr, "ua")
	if _, err := cErr.FetchUFCEvents(context.Background(), "20250115"); err == nil || !strings.Contains(err.Error(), "ESPN 500") {
		t.Fatalf("expected ESPN 500 error, got %v", err)
	}

	// malformed JSON
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("not json"))
	}))
	defer srvBad.Close()
	baseBad, _ := url.Parse(srvBad.URL)
	httpcBad := &http.Client{Transport: &rewriteTransport{base: baseBad}}
	cBad := NewClient(httpcBad, "ua")
	if _, err := cBad.FetchUFCEvents(context.Background(), "20250115"); err == nil {
		t.Fatalf("expected json decode error")
	}
}
