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
	// Keep requested path; our test server handles multiple endpoints.
	u.Path = req.URL.Path
	req2 := req.Clone(req.Context())
	req2.URL = &u
	req2.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(req2)
}

func TestFetchUFCScoreboardRoot_UsesYearAndHeaders(t *testing.T) {
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
			"events": []map[string]any{{
				"id": "1", "name": "UFC Test", "shortName": "UT", "date": "2025-01-15T20:00:00Z",
			}},
			"leagues": []map[string]any{{"calendar": []any{}}},
		})
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	httpc := &http.Client{Transport: &rewriteTransport{base: base}}
	c := NewClient(httpc, "test-agent")

	root, err := c.FetchUFCScoreboardRoot(context.Background(), "2025")
	if err != nil {
		t.Fatalf("FetchUFCScoreboardRoot error: %v", err)
	}
	if len(root.Events) != 1 || root.Events[0].ID != "1" || root.Events[0].Name != "UFC Test" {
		t.Fatalf("unexpected events: %+v", root.Events)
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

func TestFetchUFCScoreboardRoot_Errors(t *testing.T) {
	// non-2xx
	srvErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte("server exploded"))
	}))
	defer srvErr.Close()
	baseErr, _ := url.Parse(srvErr.URL)
	httpcErr := &http.Client{Transport: &rewriteTransport{base: baseErr}}
	cErr := NewClient(httpcErr, "ua")
	if _, err := cErr.FetchUFCScoreboardRoot(context.Background(), "2025"); err == nil || !strings.Contains(err.Error(), "ESPN 500") {
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
	if _, err := cBad.FetchUFCScoreboardRoot(context.Background(), "2025"); err == nil {
		t.Fatalf("expected json decode error")
	}
}

func TestFetchUFCCardForEvent_BuildsBouts(t *testing.T) {
	// Test server returns competition list, then each competition with competitors,
	// and athlete details with display names.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/competitions") && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]string{{"$ref": "/comp/1"}, {"$ref": "/comp/2"}},
			})
		case r.URL.Path == "/comp/1":
			json.NewEncoder(w).Encode(map[string]any{
				"type":        map[string]any{"text": "Lightweight"},
				"competitors": []map[string]any{{"athlete": map[string]string{"$ref": "/ath/1"}}, {"athlete": map[string]string{"$ref": "/ath/2"}}},
			})
		case r.URL.Path == "/comp/2":
			json.NewEncoder(w).Encode(map[string]any{
				"type":        map[string]any{"text": "Featherweight"},
				"competitors": []map[string]any{{"athlete": map[string]string{"$ref": "/ath/3"}}, {"athlete": map[string]string{"$ref": "/ath/4"}}},
			})
		case strings.HasPrefix(r.URL.Path, "/ath/"):
			id := strings.TrimPrefix(r.URL.Path, "/ath/")
			json.NewEncoder(w).Encode(map[string]any{"displayName": "Ath" + id})
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	base, _ := url.Parse(srv.URL)
	httpc := &http.Client{Transport: &rewriteTransport{base: base}}
	c := NewClient(httpc, "ua")

	bouts, err := c.FetchUFCCardForEvent(context.Background(), "12345")
	if err != nil {
		t.Fatalf("FetchUFCCardForEvent error: %v", err)
	}
	if len(bouts) != 2 {
		t.Fatalf("expected 2 bouts, got %d", len(bouts))
	}
	if bouts[0].Fighter1 == "" || bouts[0].Fighter2 == "" || bouts[0].WeightClass == "" {
		t.Fatalf("unexpected first bout: %+v", bouts[0])
	}
}
