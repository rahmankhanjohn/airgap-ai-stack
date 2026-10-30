// Package egress probes whether the host can reach the internet.
// Auditors run it inside the enclave to attest that no egress exists.
package egress

import (
	"context"
	"fmt"
	"net"
	"sort"
	"time"
)

// Result is one probe outcome.
type Result struct {
	Probe   string `json:"probe"`
	Target  string `json:"target"`
	OK      bool   `json:"ok"`
	Latency string `json:"latency,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Summary is the full egress report.
type Summary struct {
	At      time.Time `json:"at"`
	AirGap  bool      `json:"air_gap"`
	Probes  []Result  `json:"probes"`
	Verdict string    `json:"verdict"`
}

// Dialer abstracts connection attempts so tests can inject failures.
type Dialer interface {
	DialContext(ctx context.Context, network, addr string) (net.Conn, error)
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type realDialer struct{}

func (realDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 3 * time.Second}
	return d.DialContext(ctx, network, addr)
}

func (realDialer) LookupHost(ctx context.Context, host string) ([]string, error) {
	r := net.Resolver{}
	return r.LookupHost(ctx, host)
}

var probeTargets = []struct {
	probe  string
	target string
}{
	{"dns", "one.one.one.one"},          // DNS resolution
	{"tcp-443", "1.1.1.1:443"},          // Cloudflare
	{"tcp-443", "8.8.8.8:443"},          // Google DNS TLS
	{"tcp-80", "example.com:80"},        // plain HTTP path
}

// Check runs all probes with the given timeout budget.
// A probe "succeeds" if the connection or lookup works, which means egress EXISTS.
func Check(timeout time.Duration) Summary {
	return CheckWith(timeout, realDialer{})
}

// CheckWith runs probes against an injectable dialer.
func CheckWith(timeout time.Duration, d Dialer) Summary {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var results []Result
	for _, t := range probeTargets {
		start := time.Now()
		res := Result{Probe: t.probe, Target: t.target}
		switch t.probe {
		case "dns":
			_, err := d.LookupHost(ctx, t.target)
			res.OK = err == nil
			if err != nil {
				res.Error = truncate(err.Error())
			}
		default:
			conn, err := d.DialContext(ctx, "tcp", t.target)
			res.OK = err == nil
			if err != nil {
				res.Error = truncate(err.Error())
			} else {
				conn.Close()
			}
		}
		if res.OK {
			res.Latency = time.Since(start).Round(time.Millisecond).String()
		}
		results = append(results, res)
	}

	airGap := true
	anyOK := false
	for _, r := range results {
		if r.OK {
			airGap = false
			anyOK = true
		}
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Probe < results[j].Probe })
	verdict := "NO EGRESS DETECTED - air-gap behaviour confirmed on all probes"
	if !airGap {
		if anyOK {
			verdict = "EGRESS DETECTED - this host reaches the internet"
		}
	}
	// distinguish full vs partial for the accreditation record
	if !airGap {
		ok := 0
		for _, r := range results {
			if r.OK {
				ok++
			}
		}
		if ok < len(results) {
			verdict = "PARTIAL EGRESS DETECTED - some probes reachable; review the table"
		}
	}
	return Summary{At: time.Now().UTC(), AirGap: airGap, Probes: results, Verdict: verdict}
}

func truncate(s string) string {
	if len(s) > 120 {
		return s[:120]
	}
	return fmt.Sprintf("%s", s)
}
