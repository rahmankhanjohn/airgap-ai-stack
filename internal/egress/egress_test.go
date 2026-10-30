package egress

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// failingDialer simulates an air-gapped host: every probe fails.
type failingDialer struct{}

func (failingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return nil, errors.New("network is unreachable")
}
func (failingDialer) LookupHost(ctx context.Context, host string) ([]string, error) {
	return nil, errors.New("no such host")
}

// workingDialer simulates a host with egress: every probe succeeds.
type workingDialer struct{}

func (workingDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	return dummyConn{}, nil
}
func (workingDialer) LookupHost(ctx context.Context, host string) ([]string, error) {
	return []string{"203.0.113.1"}, nil
}

func TestCheckAirGapped(t *testing.T) {
	sum := CheckWith(1*time.Second, failingDialer{})
	if !sum.AirGap {
		t.Fatalf("expected air-gap confirmed, got %+v", sum)
	}
	if len(sum.Probes) != len(probeTargets) {
		t.Fatalf("expected %d probes, got %d", len(probeTargets), len(sum.Probes))
	}
	for _, p := range sum.Probes {
		if p.OK {
			t.Fatalf("probe %s should fail on an air-gapped host", p.Probe)
		}
		if p.Error == "" {
			t.Fatalf("probe %s should carry an error detail", p.Probe)
		}
	}
	if sum.Verdict != "NO EGRESS DETECTED - air-gap behaviour confirmed on all probes" {
		t.Fatalf("unexpected verdict: %s", sum.Verdict)
	}
}

func TestCheckEgressDetected(t *testing.T) {
	// wrap working dialer so Dial returns a real closable dummy
	d := connDialer{workingDialer{}}
	sum := CheckWith(1*time.Second, d)
	if sum.AirGap {
		t.Fatalf("expected egress detected, got %+v", sum)
	}
	if sum.Verdict != "EGRESS DETECTED - this host reaches the internet" && sum.Verdict != "PARTIAL EGRESS DETECTED - some probes reachable; review the table" {
		t.Fatalf("unexpected verdict: %s", sum.Verdict)
	}
}

type connDialer struct{ inner Dialer }

func (c connDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	conn, err := c.inner.DialContext(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	return conn, nil
}
func (c connDialer) LookupHost(ctx context.Context, host string) ([]string, error) {
	return c.inner.LookupHost(ctx, host)
}

type dummyConn struct{ net.Conn }

func (dummyConn) Close() error                       { return nil }
func (dummyConn) Read(b []byte) (int, error)         { return 0, nil }
func (dummyConn) Write(b []byte) (int, error)        { return len(b), nil }
func (dummyConn) LocalAddr() net.Addr                { return nil }
func (dummyConn) RemoteAddr() net.Addr               { return nil }
func (dummyConn) SetDeadline(t time.Time) error      { return nil }
func (dummyConn) SetReadDeadline(t time.Time) error  { return nil }
func (dummyConn) SetWriteDeadline(t time.Time) error { return nil }

func TestCheckPartialEgress(t *testing.T) {
	// one probe succeeds, rest fail: host is NOT air-gapped
	d := partialDialer{}
	sum := CheckWith(1*time.Second, d)
	if sum.AirGap {
		t.Fatal("partial egress must not count as air-gapped")
	}
}

type partialDialer struct{}

func (partialDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	if addr == "1.1.1.1:443" {
		return dummyConn{}, nil
	}
	return nil, errors.New("blocked")
}
func (partialDialer) LookupHost(ctx context.Context, host string) ([]string, error) {
	return nil, errors.New("no such host")
}
