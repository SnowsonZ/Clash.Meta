package statistic

import (
	"sort"

	"github.com/metacubex/mihomo/common/atomic"
	"github.com/metacubex/mihomo/common/xsync"
	"github.com/metacubex/mihomo/component/resolver"
	C "github.com/metacubex/mihomo/constant"
)

type RequestNotify func(c Tracker)

var DefaultRequestNotify RequestNotify

func (m *Manager) TotalTraffic(onlyProxy bool) (up, down int64) {
	if onlyProxy {
		return m.proxyUploadTotal.Load(), m.proxyDownloadTotal.Load()
	}
	return m.uploadTotal.Load(), m.downloadTotal.Load()
}

func (m *Manager) NowTraffic(onlyProxy bool) (up, down int64) {
	if onlyProxy {
		return m.proxyUploadBlip.Load(), m.proxyDownloadBlip.Load()
	}
	return m.uploadBlip.Load(), m.downloadBlip.Load()
}

type KeyStat struct {
	Key           string `json:"key"`
	Upload        int64  `json:"upload"`
	Download      int64  `json:"download"`
	ProxyUpload   int64  `json:"proxyUpload"`
	ProxyDownload int64  `json:"proxyDownload"`
	Connections   int64  `json:"connections"`
}

type TrafficKeyStats struct {
	Process []*KeyStat `json:"process"`
	Host    []*KeyStat `json:"host"`
}

type keyCounter struct {
	upload        atomic.Int64
	download      atomic.Int64
	proxyUpload   atomic.Int64
	proxyDownload atomic.Int64
	connections   atomic.Int64
}

func (c *keyCounter) add(lastChain string, upload, download int64) {
	if upload > 0 {
		c.upload.Add(upload)
		if lastChain != "DIRECT" {
			c.proxyUpload.Add(upload)
		}
	}
	if download > 0 {
		c.download.Add(download)
		if lastChain != "DIRECT" {
			c.proxyDownload.Add(download)
		}
	}
}

func addKeyStats(process, host *keyCounter, lastChain string, upload, download int64) {
	if upload <= 0 && download <= 0 {
		return
	}
	process.add(lastChain, upload, download)
	host.add(lastChain, upload, download)
}

func (m *Manager) keyCounter(stats *xsync.Map[string, *keyCounter], key string) *keyCounter {
	if counter, ok := stats.Load(key); ok {
		return counter
	}
	actual, _ := stats.LoadOrStore(key, &keyCounter{})
	return actual
}

// registerKeys derives the two aggregation keys of a new connection and counts
// it once per key. An empty process name, or a destination that is only an
// unrestored fake-ip, aggregates under the empty key.
func (m *Manager) registerKeys(metadata *C.Metadata) (*keyCounter, *keyCounter) {
	process := m.keyCounter(&m.processStats, metadata.Process)
	host := m.keyCounter(&m.hostStats, hostKey(metadata))
	process.connections.Add(1)
	host.connections.Add(1)
	return process, host
}

func hostKey(metadata *C.Metadata) string {
	if metadata.Host != "" {
		return metadata.Host
	}
	dstIP := metadata.DstIP
	if !dstIP.IsValid() || resolver.IsFakeIP(dstIP) {
		return ""
	}
	return dstIP.String()
}

func (m *Manager) SnapshotKeyStats() *TrafficKeyStats {
	return &TrafficKeyStats{
		Process: snapshotKeyStats(&m.processStats),
		Host:    snapshotKeyStats(&m.hostStats),
	}
}

func snapshotKeyStats(stats *xsync.Map[string, *keyCounter]) []*KeyStat {
	list := []*KeyStat{}
	stats.Range(func(key string, counter *keyCounter) bool {
		list = append(list, &KeyStat{
			Key:           key,
			Upload:        counter.upload.Load(),
			Download:      counter.download.Load(),
			ProxyUpload:   counter.proxyUpload.Load(),
			ProxyDownload: counter.proxyDownload.Load(),
			Connections:   counter.connections.Load(),
		})
		return true
	})
	sort.Slice(list, func(i, j int) bool {
		if list[i].Download != list[j].Download {
			return list[i].Download > list[j].Download
		}
		return list[i].Upload > list[j].Upload
	})
	return list
}

func (m *Manager) resetKeyStats() {
	m.processStats.Range(func(key string, _ *keyCounter) bool {
		m.processStats.Delete(key)
		return true
	})
	m.hostStats.Range(func(key string, _ *keyCounter) bool {
		m.hostStats.Delete(key)
		return true
	})
}
