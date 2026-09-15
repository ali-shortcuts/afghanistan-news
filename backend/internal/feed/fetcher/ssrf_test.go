package fetcher

import (
	"context"
	"net"
	"testing"
)

func TestSSRFGuardBlocksPrivateAddresses(t *testing.T) {
	g := &SSRFGuard{}
	blocked := []string{
		"127.0.0.1", "10.1.2.3", "172.16.5.5", "192.168.1.10", "169.254.169.254",
		"0.0.0.0", "100.64.0.1", "::1", "fd00::1", "fe80::1",
	}
	for _, raw := range blocked {
		if err := g.CheckIP(net.ParseIP(raw)); err == nil {
			t.Errorf("expected %s to be blocked", raw)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "93.184.216.34", "2606:4700::1111"}
	for _, raw := range allowed {
		if err := g.CheckIP(net.ParseIP(raw)); err != nil {
			t.Errorf("expected %s to be allowed: %v", raw, err)
		}
	}
}

func TestSSRFGuardBlocksInternalHostsAndPorts(t *testing.T) {
	g := &SSRFGuard{}
	cases := []string{
		"localhost:80", "metadata.google.internal:80", "db.internal:443",
		"printer.local:80", "example.com:22", "example.com:5432", "example.com:6379",
	}
	for _, host := range cases {
		if err := g.CheckHost(context.Background(), host); err == nil {
			t.Errorf("expected %s to be blocked", host)
		}
	}
}

func TestSSRFGuardRejectsUnresolvableHostSafely(t *testing.T) {
	g := &SSRFGuard{}
	if err := g.CheckHost(context.Background(), "this-domain-should-not-exist-afnews.invalid:443"); err == nil {
		t.Error("unresolvable host must produce an error, not a silent pass")
	}
}

func TestSSRFGuardAllowPrivateEscapeHatch(t *testing.T) {
	g := &SSRFGuard{AllowPrivate: true}
	if err := g.CheckIP(net.ParseIP("127.0.0.1")); err != nil {
		t.Errorf("test escape hatch should allow loopback: %v", err)
	}
}

func TestDomainLimiterSerializesRequests(t *testing.T) {
	l := newDomainLimiter(1, 1000)
	ctx := context.Background()
	if err := l.Acquire(ctx, "www.example.com"); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = l.Acquire(ctx, "example.com") // same registrable domain
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("two concurrent requests were allowed for one domain")
	default:
	}
	l.Release("example.com")
	<-done
	l.Release("example.com")
}

func TestBreakerOpensAfterRepeatedFailures(t *testing.T) {
	b := newBreaker()
	for i := 0; i < 6; i++ {
		b.Failure("flaky.example")
	}
	if err := b.Allow("flaky.example"); err == nil {
		t.Fatal("circuit should be open after repeated failures")
	}
	b.Success("flaky.example")
	if err := b.Allow("flaky.example"); err != nil {
		t.Fatalf("success should close the circuit: %v", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	if d := parseRetryAfter("120"); d.Seconds() != 120 {
		t.Errorf("seconds form = %v", d)
	}
	if d := parseRetryAfter(""); d != 0 {
		t.Errorf("empty retry-after = %v", d)
	}
}
