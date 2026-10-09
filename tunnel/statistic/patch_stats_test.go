package statistic

import (
	"net/netip"
	"testing"

	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
)

func TestKeyCounterSplitsProxyAndDirect(t *testing.T) {
	counter := &keyCounter{}
	counter.add("DIRECT", 100, 200)
	counter.add("ProxyA", 50, 150)

	if counter.upload.Load() != 150 || counter.download.Load() != 350 {
		t.Fatalf("totals = %d/%d, want 150/350", counter.upload.Load(), counter.download.Load())
	}
	if counter.proxyUpload.Load() != 50 || counter.proxyDownload.Load() != 150 {
		t.Fatalf("proxy = %d/%d, want 50/150", counter.proxyUpload.Load(), counter.proxyDownload.Load())
	}
}

func TestAddKeyStatsSkipsEmpty(t *testing.T) {
	process := &keyCounter{}
	host := &keyCounter{}
	addKeyStats(process, host, "DIRECT", 0, 0)
	if process.upload.Load() != 0 || host.upload.Load() != 0 {
		t.Fatal("zero-size push must be a no-op")
	}
}

type stubMapper struct {
	resolver.Enhancer
}

func (stubMapper) IsFakeIP(netip.Addr) bool {
	return true
}

func withFakeIPMapper(t *testing.T) {
	t.Helper()
	previous := resolver.DefaultHostMapper
	resolver.DefaultHostMapper = stubMapper{}
	t.Cleanup(func() {
		resolver.DefaultHostMapper = previous
	})
}

func TestHostKeyPreference(t *testing.T) {
	host, err := netip.ParseAddr("1.2.3.4")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		metadata *C.Metadata
		want     string
	}{
		{"host wins", &C.Metadata{Host: "a.com", DstIP: host}, "a.com"},
		{"ip fallback", &C.Metadata{DstIP: host}, "1.2.3.4"},
		{"no destination", &C.Metadata{}, ""},
	}
	for _, cs := range cases {
		if got := hostKey(cs.metadata); got != cs.want {
			t.Errorf("%s: hostKey = %q, want %q", cs.name, got, cs.want)
		}
	}
}

func TestHostKeyDropsFakeIP(t *testing.T) {
	withFakeIPMapper(t)
	fake, err := netip.ParseAddr("198.18.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if got := hostKey(&C.Metadata{DstIP: fake}); got != "" {
		t.Fatalf("hostKey = %q, want empty for fake-ip", got)
	}
}

func TestRegisterAndSnapshot(t *testing.T) {
	withFakeIPMapper(t)
	manager := &Manager{}

	processStat, hostStat := manager.registerKeys(&C.Metadata{Process: "curl", Host: "a.com"})
	processStat.add("ProxyA", 10, 100)
	hostStat.add("ProxyA", 10, 100)

	secondProcess, secondHost := manager.registerKeys(&C.Metadata{Process: "curl", Host: "a.com"})
	secondProcess.add("DIRECT", 5, 0)
	secondHost.add("DIRECT", 5, 0)

	manager.registerKeys(&C.Metadata{Process: "", Host: ""})

	stats := manager.SnapshotKeyStats()
	if len(stats.Process) != 2 || len(stats.Host) != 2 {
		t.Fatalf("process = %d rows, host = %d rows, want 2/2", len(stats.Process), len(stats.Host))
	}

	curl := stats.Process[0]
	if curl.Key != "curl" {
		t.Fatalf("top process = %q, want curl", curl.Key)
	}
	if curl.Upload != 15 || curl.Download != 100 {
		t.Fatalf("curl = %d/%d, want 15/100", curl.Upload, curl.Download)
	}
	if curl.ProxyUpload != 10 || curl.ProxyDownload != 100 {
		t.Fatalf("curl proxy = %d/%d, want 10/100", curl.ProxyUpload, curl.ProxyDownload)
	}
	if curl.Connections != 2 {
		t.Fatalf("curl connections = %d, want 2", curl.Connections)
	}

	site := stats.Host[0]
	if site.Key != "a.com" || site.Connections != 2 {
		t.Fatalf("host row = %+v, want a.com with 2 connections", site)
	}

	manager.resetKeyStats()
	if stats = manager.SnapshotKeyStats(); len(stats.Process) != 0 || len(stats.Host) != 0 {
		t.Fatal("resetKeyStats left rows behind")
	}
}
