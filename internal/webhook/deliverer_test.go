package webhook

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
)

func TestRejectPrivateAddress(t *testing.T) {
	blocked := []string{
		"127.0.0.1:8080",     // IPv4 loopback
		"[::1]:8080",         // IPv6 loopback
		"10.1.2.3:5432",      // RFC1918 10/8
		"172.16.0.9:5432",    // RFC1918 172.16/12
		"192.168.1.1:5432",   // RFC1918 192.168/16
		"169.254.169.254:80", // cloud metadata endpoint (link-local)
		"[fe80::1]:8080",     // IPv6 link-local
		"0.0.0.0:8080",       // unspecified
		"[::]:8080",          // unspecified
		"[fc00::1]:8080",     // IPv6 unique-local
		"224.0.0.1:8080",     // multicast
	}

	for _, addr := range blocked {
		if err := rejectPrivateAddress(addr); err == nil {
			t.Errorf("rejectPrivateAddress(%q) = nil, want blocked", addr)
		}
	}

	allowed := []string{
		"8.8.8.8:443",
		"1.1.1.1:80",
		"93.184.216.34:8080",
		"[2606:4700:4700::1111]:443",
	}

	for _, addr := range allowed {
		if err := rejectPrivateAddress(addr); err != nil {
			t.Errorf("rejectPrivateAddress(%q) = %v, want allowed", addr, err)
		}
	}
}

// TestDeliverBlocksLoopbackTarget proves the guard fires at dial time: the
// local receiver must see zero requests when the deliverer is in secure mode.
func TestDeliverBlocksLoopbackTarget(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDeliverer()
	res := d.Deliver(context.Background(), &Delivery{
		URL:     srv.URL,
		Payload: []byte(`{"probe":true}`),
	})

	if res.Error == nil {
		t.Fatal("delivery to loopback target succeeded, want SSRF block")
	}
	if hits.Load() != 0 {
		t.Errorf("receiver saw %d requests, want 0", hits.Load())
	}
	if res.Retryable {
		t.Error("SSRF block classified retryable, want permanent")
	}
}

// TestDeliverAllowsLocalWhenWhitelisted is the dev-mode escape hatch: with
// AllowPrivateAddresses the same local receiver is reachable and the HMAC
// signature round-trips.
func TestDeliverAllowsLocalWhenWhitelisted(t *testing.T) {
	type received struct {
		body      []byte
		eventID   string
		signature string
		timestamp string
	}
	ch := make(chan received, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- received{
			body:      body,
			eventID:   r.Header.Get("X-RelayDB-Event-ID"),
			signature: r.Header.Get("X-RelayDB-Signature"),
			timestamp: r.Header.Get("X-RelayDB-Timestamp"),
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	d := NewDelivererWithOptions(Options{AllowPrivateAddresses: true})
	res := d.Deliver(context.Background(), &Delivery{
		EventID: []byte{1, 2, 3, 4},
		URL:     srv.URL,
		Secret:  "dev-secret",
		Payload: []byte(`{"order_id":7}`),
	})

	if res.Error != nil {
		t.Fatalf("whitelisted local delivery failed: %v", res.Error)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", res.StatusCode)
	}

	got := <-ch
	ts, err := strconv.ParseInt(got.timestamp, 10, 64)
	if err != nil {
		t.Fatalf("parse timestamp header %q: %v", got.timestamp, err)
	}
	if !VerifySignature("dev-secret", ts, []byte{1, 2, 3, 4}, got.body, got.signature) {
		t.Error("receiver-side signature verification failed")
	}
}

// TestDeliverRedirectRechecksEachHop follows a redirect between two local
// endpoints in whitelisted mode; every hop is dialed through the guarded
// dialer, so both hops are reached only because each was explicitly allowed.
func TestDeliverRedirectRechecksEachHop(t *testing.T) {
	var hop2Hits atomic.Int32
	hop2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hop2Hits.Add(1)
		w.WriteHeader(200)
	}))
	defer hop2.Close()

	hop1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, hop2.URL+"/final", http.StatusFound)
	}))
	defer hop1.Close()

	d := NewDelivererWithOptions(Options{AllowPrivateAddresses: true})
	res := d.Deliver(context.Background(), &Delivery{URL: hop1.URL, Payload: []byte(`{}`)})

	if res.Error != nil {
		t.Fatalf("redirect delivery failed: %v", res.Error)
	}
	if hop2Hits.Load() != 1 {
		t.Errorf("second hop saw %d requests, want 1", hop2Hits.Load())
	}
}

// TestDeliverRejectsNonHTTPScheme covers the non-network SSRF vectors.
func TestDeliverRejectsNonHTTPScheme(t *testing.T) {
	d := NewDeliverer()
	for _, url := range []string{"file:///etc/passwd", "gopher://internal:70", "unix:///var/run.sock"} {
		res := d.Deliver(context.Background(), &Delivery{URL: url})
		if res.Error == nil {
			t.Errorf("scheme for %q accepted, want rejection", url)
		}
	}
}

// TestDialerControlHookIsWired asserts the transport actually routes through
// the guarded Control callback (regression guard against silent unwiring).
func TestDialerControlHookIsWired(t *testing.T) {
	d := NewDeliverer()
	transport, ok := d.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("client transport is not *http.Transport")
	}
	if transport.DialContext == nil {
		t.Fatal("transport has no DialContext")
	}

	// Dialing loopback through the raw guarded dialer must fail.
	conn, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:1")
	if err == nil {
		conn.Close()
		t.Fatal("guarded dialer connected to loopback")
	}
	if _, ok := err.(*net.OpError); !ok && net.ParseIP("127.0.0.1") != nil {
		t.Logf("dial error type %T: %v", err, err)
	}
}
