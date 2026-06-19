package metrics

import (
	"context"
	"github.com/alex4096/server-dashboard/backend/internal/server"
	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
	"github.com/shirou/gopsutil/v4/net"
	"github.com/shirou/gopsutil/v4/process"
	"time"
)

type Process struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryBytes uint64  `json:"memory_bytes"`
}
type System struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemoryTotal uint64  `json:"memory_total"`
	MemoryUsed  uint64  `json:"memory_used"`
	DiskTotal   uint64  `json:"disk_total"`
	DiskUsed    uint64  `json:"disk_used"`
}
type Network struct {
	RXBytesTotal uint64 `json:"rx_bytes_total"`
	TXBytesTotal uint64 `json:"tx_bytes_total"`
}
type Summary struct {
	Server  server.Status `json:"server"`
	Process Process       `json:"process"`
	System  System        `json:"system"`
	Network Network       `json:"network"`
}
type Collector struct {
	server *server.Manager
	root   string
}

func New(s *server.Manager, root string) *Collector { return &Collector{server: s, root: root} }
func (c *Collector) Summary(ctx context.Context) (Summary, error) {
	st, _ := c.server.Status(ctx)
	out := Summary{Server: st}
	if v, e := cpu.Percent(100*time.Millisecond, false); e == nil && len(v) > 0 {
		out.System.CPUPercent = v[0]
	}
	if v, e := mem.VirtualMemory(); e == nil {
		out.System.MemoryTotal = v.Total
		out.System.MemoryUsed = v.Used
	}
	if v, e := disk.Usage(c.root); e == nil {
		out.System.DiskTotal = v.Total
		out.System.DiskUsed = v.Used
	}
	if vv, e := net.IOCounters(false); e == nil && len(vv) > 0 {
		out.Network.RXBytesTotal = vv[0].BytesRecv
		out.Network.TXBytesTotal = vv[0].BytesSent
	}
	if st.PID > 0 {
		if p, e := process.NewProcess(int32(st.PID)); e == nil {
			out.Process.CPUPercent, _ = p.CPUPercent()
			if mi, e := p.MemoryInfo(); e == nil {
				out.Process.MemoryBytes = mi.RSS
			}
		}
	}
	return out, nil
}
