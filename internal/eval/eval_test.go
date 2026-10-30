package eval

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// mockOpenAI returns an endpoint that replies with canned answers keyed by
// a substring of the prompt, mimicking an OpenAI-compatible server.
func mockOpenAI(t *testing.T, answers map[string]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		prompt := ""
		if len(req.Messages) > 0 {
			prompt = req.Messages[len(req.Messages)-1].Content
		}
		answer := "no answer configured"
		for key, a := range answers {
			if strings.Contains(strings.ToLower(prompt), strings.ToLower(key)) {
				answer = a
				break
			}
		}
		resp := chatResponse{}
		resp.Choices = append(resp.Choices, struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		}{})
		resp.Choices[0].Message.Content = answer
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
}

func testSuite() *Suite {
	return &Suite{
		Name:  "test-suite",
		Model: "test-model",
		Cases: []Case{
			{
				ID:     "ttl-term",
				Prompt: "Users waited 10 minutes. Console said 30 seconds. Which term dominates?",
				Checks: []Check{
					{Type: "contains_any", Value: "ttl, time to live, cache"},
					{Type: "not_contains", Value: "as an AI"},
				},
			},
			{
				ID:     "health-check",
				Prompt: "What health check settings avoid false failovers on deploys?",
				Checks: []Check{
					{Type: "contains", Value: "inter 3s"},
					{Type: "regex", Value: `(?i)fall\s*3`},
				},
			},
		},
	}
}

func TestRunAllPass(t *testing.T) {
	srv := mockOpenAI(t, map[string]string{
		"10 minutes": "The remaining TTL term dominates: clients keep the cached answer.",
		"health":     "Use inter 3s, fall 3, rise 2 with a shallow check.",
	})
	defer srv.Close()
	rep, err := RunWith(testSuite(), srv.URL, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Total != 2 || rep.Passed != 2 {
		t.Fatalf("expected 2/2, got %d/%d: %+v", rep.Passed, rep.Total, rep.Results)
	}
	if rep.Score != 1.0 {
		t.Fatalf("score should be 1.0, got %f", rep.Score)
	}
}

func TestRunDetectsFailure(t *testing.T) {
	srv := mockOpenAI(t, map[string]string{
		"10 minutes": "Try rebooting the router.",
		"health":     "inter 3s, fall 3, rise 2 with a shallow check.",
	})
	defer srv.Close()
	rep, err := RunWith(testSuite(), srv.URL, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Passed != 1 || rep.Failed != 1 {
		t.Fatalf("expected 1 pass 1 fail, got %+v", rep)
	}
	if rep.Results[0].Passed {
		t.Fatal("ttl case should fail")
	}
	if len(rep.Results[0].Failures) == 0 {
		t.Fatal("failure reasons should be populated")
	}
}

func TestRunEndpointDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	rep, err := RunWith(testSuite(), srv.URL, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rep.Results {
		if r.Error == "" {
			t.Fatalf("case %s should carry an HTTP error", r.ID)
		}
	}
}

func TestLoadSuiteValidation(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/suite.json"
	if _, err := LoadSuite(p); err == nil {
		t.Fatal("missing file should error")
	}
	json := `{"name":"s","cases":[]}`
	p2 := dir + "/empty.json"
	writeFile(t, p2, json)
	if _, err := LoadSuite(p2); err == nil {
		t.Fatal("empty suite should error")
	}
	json2 := `{"name":"s","cases":[{"id":"a","prompt":"p"}]}`
	p3 := dir + "/nochecks.json"
	writeFile(t, p3, json2)
	if _, err := LoadSuite(p3); err == nil {
		t.Fatal("case without checks should error")
	}
}

func TestTimeoutHandling(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer srv.Close()
	srv.Client().Timeout = 100 * time.Millisecond
	s := &Suite{Name: "t", Cases: []Case{{ID: "slow", Prompt: "p", Checks: []Check{{Type: "contains", Value: "x"}}}}}
	rep, err := RunWith(s, srv.URL, "", srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	if rep.Results[0].Error == "" {
		t.Fatal("timeout should surface as case error")
	}
}
