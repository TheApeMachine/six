package distributed

import "time"

const (
	DefaultDiscoveryGroup = "239.42.6.1:7778"
	DefaultServicePath    = "/v1/jobs/universal-bitwise"
)

type DiscoveryMessage struct {
	Type      string `json:"type"`
	NodeID    string `json:"node_id"`
	Addr      string `json:"addr"`
	Capacity  int    `json:"capacity"`
	ShardBits uint8  `json:"shard_bits,omitempty"`
	ShardMask uint64 `json:"shard_mask,omitempty"`
	Timestamp int64  `json:"ts_unix_nano"`
}

type Node struct {
	ID        string    `json:"id"`
	Addr      string    `json:"addr"`
	Capacity  int       `json:"capacity"`
	ShardBits uint8     `json:"shard_bits,omitempty"`
	ShardMask uint64    `json:"shard_mask,omitempty"`
	LastSeen  time.Time `json:"last_seen"`
	Self      bool      `json:"self"`
}

type UniversalBitwiseJobRequest struct {
	Left  []byte `json:"left"`
	Right []byte `json:"right,omitempty"`
}

type UniversalBitwiseJobResponse struct {
	NodeID     string `json:"node_id"`
	DurationMS int64  `json:"duration_ms"`
	Left       []byte `json:"left,omitempty"`
	Right      []byte `json:"right,omitempty"`
	Error      string `json:"error,omitempty"`
}
