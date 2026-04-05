package kadabra

func (node *KadabraNode) epochPenalty(bucket *kadabraBucket) float64 {
	if node.Penalty > 0 {
		return node.Penalty
	}

	totalLatency := 0.0
	totalQueries := 0

	for _, sample := range bucket.Samples {
		totalLatency += sample.LatencyTotal
		totalQueries += sample.Queries
	}

	if totalQueries == 0 {
		return 1
	}

	return totalLatency/float64(totalQueries) + 1
}

func (node *KadabraNode) finishEpoch(bucket *kadabraBucket) {
	if bucket == nil || bucket.QueryCount == 0 {
		return
	}

	penalty := node.epochPenalty(bucket)
	currentScores := make(
		map[NodeID]float64, len(bucket.Entries),
	)

	for _, entry := range bucket.Entries {
		sample := bucket.Samples[entry.ID]
		usedQueries := 0
		latencyTotal := 0.0

		if sample != nil {
			usedQueries = sample.Queries
			latencyTotal = sample.LatencyTotal
		}

		currentScores[entry.ID] = -(latencyTotal + float64(
			bucket.QueryCount-usedQueries,
		)*penalty)
	}

	currentBucketScore := averagePeerScores(currentScores)

	if bucket.ExploreNext {
		bucket.PreviousEntries = clonePeers(bucket.Entries)
		bucket.PreviousScore = currentBucketScore

		replacement := node.selectExplorationPeer(bucket)

		if replacement != nil {
			worstPeerID := worstPeerScore(currentScores)

			for entryIndex, entry := range bucket.Entries {
				if entry.ID != worstPeerID {
					continue
				}

				bucket.Entries[entryIndex] = clonePeer(replacement)
				break
			}
			sortPeersByID(bucket.Entries)
		}

		bucket.ExploreNext = false
	} else {
		if currentBucketScore > bucket.PreviousScore || len(bucket.PreviousEntries) == 0 {
			bucket.PreviousEntries = clonePeers(bucket.Entries)
			bucket.PreviousScore = currentBucketScore
		} else {
			bucket.Entries = clonePeers(bucket.PreviousEntries)
		}

		bucket.ExploreNext = true
	}

	bucket.QueryCount = 0
	bucket.Samples = make(map[NodeID]*kadabraPeerSample)
}
