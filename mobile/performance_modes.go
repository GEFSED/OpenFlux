package mobile

import (
 "time"
 "openflux/transport"
 "openflux/transport/yandex"
)

type performanceConfig struct {
 volga yandex.VolgaConfig
 outer transport.BatchedConfig
}
func normalizeMode(name string) string {
 switch name {
 case "speed", "optimized": return name
 default: return "standard"
 }
}
func effectiveMode(kind, name string) string {
 if kind != "vyandex" { return "standard" }
 return normalizeMode(name)
}

// Frozen .8 throughput_w32 scheduling; optimized differs only in guard enable.
func modeConfig(name string) performanceConfig {
 p := performanceConfig{yandex.DefaultVolgaConfig(), transport.DefaultBatchedConfig()}
 if normalizeMode(name) == "standard" { return p }
 p.volga.WorkerCount = 32
 p.volga.QueueSize = 4096
 p.volga.BatchSize = 32
 p.volga.BatchTimeout = time.Millisecond
 p.volga.MaxIdleConnsPerHost = 32
 p.volga.MaxIdleConns = 64
 p.volga.RateLimit429GuardEnabled = name == "optimized"
 p.outer.MaxBatchBytes = 32 << 10
 p.outer.Linger = 2 * time.Millisecond
 return p
}
