// Package eval runs deterministic evaluation suites against an
// OpenAI-compatible chat endpoint and scores them without any cloud calls.
package eval

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

// Check is one deterministic assertion on the model response.
type Check struct {
	Type  string `json:"type"`  // contains | contains_any | not_contains | regex | exact
	Value string `json:"value"` // for contains_any: comma separated alternatives
}

// Case is one eval case: a prompt plus its checks.
type Case struct {
	ID     string  `json:"id"`
	Prompt string  `json:"prompt"`
	Checks []Check `json:"checks"`
	MaxTok int     `json:"max_tokens,omitempty"`
}

// Suite is a named set of cases.
type Suite struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Model       string `json:"model,omitempty"` // model name to request; empty = server default
	Cases       []Case `json:"cases"`
}

// CaseResult is the outcome of one case.
type CaseResult struct {
	ID       string   `json:"id"`
	Passed   bool     `json:"passed"`
	Response string   `json:"response_excerpt,omitempty"`
	Failures []string `json:"failures,omitempty"`
	Duration string   `json:"duration"`
	Error    string   `json:"error,omitempty"`
}

// Report aggregates the run.
type Report struct {
	Suite      string       `json:"suite"`
	Endpoint   string       `json:"endpoint"`
	Model      string       `json:"model,omitempty"`
	At         time.Time    `json:"at"`
	Passed     int          `json:"passed"`
	Failed     int          `json:"failed"`
	Total      int          `json:"total"`
	Score      float64      `json:"score"`
	Results    []CaseResult `json:"results"`
}

// LoadSuite reads and validates a suite JSON file.
func LoadSuite(path string) (*Suite, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Suite
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, fmt.Errorf("parse suite %s: %w", path, err)
	}
	if s.Name == "" {
		return nil, fmt.Errorf("suite %s has no name", path)
	}
	if len(s.Cases) == 0 {
		return nil, fmt.Errorf("suite %s has no cases", path)
	}
	for i, c := range s.Cases {
		if c.ID == "" {
			return nil, fmt.Errorf("suite %s case %d has no id", path, i)
		}
		if c.Prompt == "" {
			return nil, fmt.Errorf("suite %s case %s has no prompt", path, c.ID)
		}
		if len(c.Checks) == 0 {
			return nil, fmt.Errorf("suite %s case %s has no checks", path, c.ID)
		}
	}
	return &s, nil
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// Run executes every case against endpoint (OpenAI-compatible /v1/chat/completions).
func Run(s *Suite, endpoint, model string, timeout time.Duration) (*Report, error) {
	client := &http.Client{Timeout: timeout}
	return RunWith(s, endpoint, model, client)
}

// RunWith is Run against an injectable client for tests.
func RunWith(s *Suite, endpoint, model string, client httpClient) (*Report, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("endpoint is required")
	}
	if model == "" {
		model = s.Model
	}
	rep := &Report{Suite: s.Name, Endpoint: endpoint, Model: model, At: time.Now().UTC()}
	for _, c := range s.Cases {
		res := runCase(client, endpoint, model, c)
		rep.Results = append(rep.Results, res)
		rep.Total++
		if res.Passed {
			rep.Passed++
		} else {
			rep.Failed++
		}
	}
	if rep.Total > 0 {
		rep.Score = float64(rep.Passed) / float64(rep.Total)
	}
	return rep, nil
}

type chatRequest struct {
	Model    string `json:"model,omitempty"`
	Messages []msg  `json:"messages"`
	MaxTok   int    `json:"max_tokens,omitempty"`
	Temp     float64 `json:"temperature"`
}

type msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func runCase(client httpClient, endpoint, model string, c Case) CaseResult {
	start := time.Now()
	res := CaseResult{ID: c.ID, Duration: "0s"}

	body, _ := json.Marshal(chatRequest{
		Model:  model,
		Temp:   0,
		MaxTok: c.MaxTok,
		Messages: []msg{
			{Role: "system", Content: "You are a precise infrastructure assistant. Answer concisely and factually."},
			{Role: "user", Content: c.Prompt},
		},
	})
	req, err := http.NewRequest("POST", strings.TrimSuffix(endpoint, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		res.Error = err.Error()
		return res
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		res.Error = err.Error()
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		res.Error = fmt.Sprintf("HTTP %d: %s", resp.StatusCode, truncate(string(raw)))
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		res.Error = "decode response: " + err.Error()
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	if cr.Error != nil {
		res.Error = cr.Error.Message
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	if len(cr.Choices) == 0 {
		res.Error = "no choices in response"
		res.Duration = time.Since(start).Round(time.Millisecond).String()
		return res
	}
	answer := cr.Choices[0].Message.Content
	res.Response = truncate(strings.TrimSpace(answer))
	res.Failures = score(c.Checks, answer)
	res.Passed = len(res.Failures) == 0
	res.Duration = time.Since(start).Round(time.Millisecond).String()
	return res
}

// score applies checks; returns human-readable failure lines (empty = pass).
func score(checks []Check, answer string) []string {
	var fails []string
	lower := strings.ToLower(answer)
	for _, chk := range checks {
		switch strings.ToLower(chk.Type) {
		case "contains":
			if !strings.Contains(lower, strings.ToLower(chk.Value)) {
				fails = append(fails, fmt.Sprintf("contains %q", chk.Value))
			}
		case "contains_any":
			alts := strings.Split(chk.Value, ",")
			ok := false
			for _, a := range alts {
				if strings.Contains(lower, strings.ToLower(strings.TrimSpace(a))) {
					ok = true
					break
				}
			}
			if !ok {
				fails = append(fails, fmt.Sprintf("contains_any [%s]", chk.Value))
			}
		case "not_contains":
			if strings.Contains(lower, strings.ToLower(chk.Value)) {
				fails = append(fails, fmt.Sprintf("not_contains %q", chk.Value))
			}
		case "regex":
			re, err := regexp.Compile(chk.Value)
			if err != nil {
				fails = append(fails, fmt.Sprintf("regex compile error: %v", err))
				continue
			}
			if !re.MatchString(answer) {
				fails = append(fails, fmt.Sprintf("regex %q", chk.Value))
			}
		case "exact":
			if strings.TrimSpace(answer) != chk.Value {
				fails = append(fails, fmt.Sprintf("exact %q", chk.Value))
			}
		default:
			fails = append(fails, fmt.Sprintf("unknown check type %q", chk.Type))
		}
	}
	return fails
}

func truncate(s string) string {
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
