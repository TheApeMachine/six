package kadabra

/*
runGossipWorker is the sole consumer for enqueueGossip: it runs Gossip
serially so epoch increments and digest reads are not racy.
*/
func (node *KadabraNode) runGossipWorker() {
	if node == nil {
		return
	}

	for range node.gossipCh {
		node.Gossip()
	}
}

/*
enqueueGossip notifies the gossip worker. Buffered channel avoids blocking
finishEpoch while bucket.mu is held; capacity absorbs bursts of epoch ends.
*/
func (node *KadabraNode) enqueueGossip() {
	if node == nil || node.gossipCh == nil {
		return
	}

	node.gossipCh <- struct{}{}
}
