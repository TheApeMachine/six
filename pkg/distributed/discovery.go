package distributed

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/theapemachine/six/pkg/errnie"
	"github.com/theapemachine/six/pkg/network"
)

type discoveryOption func(*Discovery)

type Discovery struct {
	ctx    context.Context
	cancel context.CancelFunc

	nodeID       string
	advertise    string
	discoveryGrp string
	iface        string
	capacity     int
	heartbeat    time.Duration
	ttl          time.Duration
	announce     bool

	mu    sync.RWMutex
	nodes map[string]Node

	rx *network.UDPMulticast
	tx *network.UDPMulticast
}

func NewDiscovery(ctx context.Context, opts ...discoveryOption) *Discovery {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)

	d := &Discovery{
		ctx:          ctx,
		cancel:       cancel,
		nodeID:       uuid.NewString(),
		discoveryGrp: DefaultDiscoveryGroup,
		heartbeat:    time.Second,
		ttl:          5 * time.Second,
		capacity:     1,
		announce:     true,
		nodes:        make(map[string]Node),
	}

	for _, opt := range opts {
		opt(d)
	}

	if d.heartbeat <= 0 {
		d.heartbeat = time.Second
	}
	if d.ttl <= d.heartbeat {
		d.ttl = 5 * d.heartbeat
	}
	if d.capacity < 0 {
		d.capacity = 0
	}

	d.upsertNode(Node{
		ID:       d.nodeID,
		Addr:     d.advertise,
		Capacity: d.capacity,
		LastSeen: time.Now(),
		Self:     true,
	})

	return d
}

func DiscoveryWithNodeID(nodeID string) discoveryOption {
	return func(d *Discovery) {
		nodeID = strings.TrimSpace(nodeID)
		if nodeID != "" {
			d.nodeID = nodeID
		}
	}
}

func DiscoveryWithAdvertiseAddr(addr string) discoveryOption {
	return func(d *Discovery) {
		d.advertise = strings.TrimSpace(addr)
	}
}

func DiscoveryWithGroup(group string) discoveryOption {
	return func(d *Discovery) {
		group = strings.TrimSpace(group)
		if group != "" {
			d.discoveryGrp = group
		}
	}
}

func DiscoveryWithInterface(iface string) discoveryOption {
	return func(d *Discovery) {
		d.iface = strings.TrimSpace(iface)
	}
}

func DiscoveryWithHeartbeat(interval time.Duration) discoveryOption {
	return func(d *Discovery) {
		d.heartbeat = interval
	}
}

func DiscoveryWithTTL(ttl time.Duration) discoveryOption {
	return func(d *Discovery) {
		d.ttl = ttl
	}
}

func DiscoveryWithCapacity(capacity int) discoveryOption {
	return func(d *Discovery) {
		if capacity < 0 {
			capacity = 0
		}
		d.capacity = capacity
	}
}

func DiscoveryWithAnnounce(enabled bool) discoveryOption {
	return func(d *Discovery) {
		d.announce = enabled
	}
}

func (d *Discovery) Start() error {
	d.rx = network.NewUDPMulticast(network.UDPMulticastWithListener(d.discoveryGrp, d.iface))
	if d.rx == nil || d.rx.Ready(d.ctx) != nil {
		return fmt.Errorf("distributed discovery listener: failed to bind %s", d.discoveryGrp)
	}

	d.tx = network.NewUDPMulticast(network.UDPMulticastWithDialer(d.discoveryGrp))
	if d.tx == nil || d.tx.Ready(d.ctx) != nil {
		_ = d.rx.Close()
		return fmt.Errorf("distributed discovery dialer: failed to bind %s", d.discoveryGrp)
	}

	go d.recvLoop()
	go d.announceLoop()
	go d.pruneLoop()

	return nil
}

func (d *Discovery) Close() error {
	if d.cancel != nil {
		d.cancel()
	}

	var errs []error
	if d.rx != nil {
		if err := d.rx.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if d.tx != nil {
		if err := d.tx.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	return errorsJoin(errs)
}

func (d *Discovery) Nodes(includeSelf bool) []Node {
	d.mu.RLock()
	defer d.mu.RUnlock()

	nodes := make([]Node, 0, len(d.nodes))
	for _, n := range d.nodes {
		if !includeSelf && n.Self {
			continue
		}
		nodes = append(nodes, n)
	}
	slices.SortFunc(nodes, func(a, b Node) int {
		if a.Self != b.Self {
			if a.Self {
				return -1
			}
			return 1
		}
		return strings.Compare(a.ID, b.ID)
	})
	return nodes
}

func (d *Discovery) Self() Node {
	d.mu.RLock()
	defer d.mu.RUnlock()
	if n, ok := d.nodes[d.nodeID]; ok {
		return n
	}
	return Node{
		ID:       d.nodeID,
		Addr:     d.advertise,
		Capacity: d.capacity,
		LastSeen: time.Now(),
		Self:     true,
	}
}

func (d *Discovery) Capacity() int {
	if d == nil {
		return 0
	}

	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.capacity
}

func (d *Discovery) SetCapacity(capacity int) int {
	if d == nil {
		return 0
	}
	if capacity < 0 {
		capacity = 0
	}

	d.mu.Lock()
	d.capacity = capacity

	self, ok := d.nodes[d.nodeID]
	if !ok {
		self = Node{
			ID:   d.nodeID,
			Addr: d.advertise,
			Self: true,
		}
	}

	self.Capacity = capacity
	self.LastSeen = time.Now()
	self.Self = true
	d.nodes[d.nodeID] = self
	d.mu.Unlock()

	d.sendHeartbeat()
	return capacity
}

func (d *Discovery) recvLoop() {
	buf := make([]byte, 1500)
	for {
		select {
		case <-d.ctx.Done():
			return
		default:
		}

		n, err := d.rx.Read(buf)
		if err != nil {
			if d.ctx.Err() != nil {
				return
			}
			errnie.Debug("distributed.discovery.recv", "err", err)
			continue
		}

		var msg DiscoveryMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		if msg.Type != "heartbeat" || msg.NodeID == "" {
			continue
		}

		node := Node{
			ID:       msg.NodeID,
			Addr:     strings.TrimSpace(msg.Addr),
			Capacity: msg.Capacity,
			LastSeen: time.Now(),
			Self:     msg.NodeID == d.nodeID,
		}
		if node.Capacity < 0 {
			node.Capacity = 0
		}
		d.upsertNode(node)
	}
}

func (d *Discovery) announceLoop() {
	if !d.announce {
		<-d.ctx.Done()
		return
	}

	t := time.NewTicker(d.heartbeat)
	defer t.Stop()
	d.sendHeartbeat()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
			d.sendHeartbeat()
		}
	}
}

func (d *Discovery) pruneLoop() {
	t := time.NewTicker(d.heartbeat)
	defer t.Stop()

	for {
		select {
		case <-d.ctx.Done():
			return
		case <-t.C:
			cutoff := time.Now().Add(-d.ttl)
			d.mu.Lock()
			for id, n := range d.nodes {
				if n.Self {
					continue
				}
				if n.LastSeen.Before(cutoff) {
					delete(d.nodes, id)
				}
			}
			d.mu.Unlock()
		}
	}
}

func (d *Discovery) sendHeartbeat() {
	self := d.Self()
	msg := DiscoveryMessage{
		Type:      "heartbeat",
		NodeID:    self.ID,
		Addr:      self.Addr,
		Capacity:  self.Capacity,
		Timestamp: time.Now().UnixNano(),
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return
	}
	if d.tx != nil {
		if _, err := d.tx.Write(body); err != nil {
			errnie.Debug("distributed.discovery.announce", "err", err)
		}
	}
}

func (d *Discovery) upsertNode(n Node) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if n.LastSeen.IsZero() {
		n.LastSeen = time.Now()
	}
	d.nodes[n.ID] = n
}

func ResolveAdvertiseAddr(listenAddr string) (string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(listenAddr))
	if err != nil {
		return "", err
	}

	host = strings.TrimSpace(host)
	if host == "" || host == "0.0.0.0" || host == "::" {
		ip, err := firstUsableIPv4()
		if err != nil {
			return "", err
		}
		host = ip
	}

	return net.JoinHostPort(host, port), nil
}

func firstUsableIPv4() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			ip = ip.To4()
			if ip == nil || ip.IsLoopback() {
				continue
			}
			return ip.String(), nil
		}
	}
	return "", fmt.Errorf("no usable IPv4 address found")
}

func errorsJoin(errs []error) error {
	var out error
	for _, err := range errs {
		if err == nil {
			continue
		}
		if out == nil {
			out = err
			continue
		}
		out = fmt.Errorf("%v; %w", out, err)
	}
	return out
}
