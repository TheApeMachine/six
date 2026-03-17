package vm

/*
SpanIndex resolves exact prompt prefixes against ingested samples.
It selects the continuation with the longest remaining span and breaks ties
by original sample order.
*/
type SpanIndex struct {
	root    *spanNode
	samples [][]byte
}

type spanNode struct {
	children   map[byte]*spanNode
	sample     int
	suffixFrom int
}

/*
NewSpanIndex instantiates an exact prefix index for prompt recall.
*/
func NewSpanIndex() *SpanIndex {
	return &SpanIndex{
		root: &spanNode{
			sample: -1,
		},
	}
}

/*
Reset clears all indexed samples and prefix state.
*/
func (spanIndex *SpanIndex) Reset() {
	spanIndex.root = &spanNode{
		sample: -1,
	}
	spanIndex.samples = nil
}

/*
Ingest indexes one sample for exact prefix continuation.
*/
func (spanIndex *SpanIndex) Ingest(sample []byte) {
	cloned := append([]byte(nil), sample...)
	sampleIndex := len(spanIndex.samples)
	spanIndex.samples = append(spanIndex.samples, cloned)
	spanIndex.observe(spanIndex.root, sampleIndex, 0)

	node := spanIndex.root

	for offset, symbol := range cloned {
		node = spanIndex.advance(node, symbol)
		spanIndex.observe(node, sampleIndex, offset+1)
	}
}

/*
Resolve returns the exact continuation for query.
If the query is not an indexed prefix, it returns nil.
*/
func (spanIndex *SpanIndex) Resolve(query []byte) []byte {
	node := spanIndex.root

	for _, symbol := range query {
		if node.children == nil {
			return nil
		}

		next, ok := node.children[symbol]
		if !ok {
			return nil
		}

		node = next
	}

	if node.sample < 0 {
		return nil
	}

	sample := spanIndex.samples[node.sample]
	continuation := sample[node.suffixFrom:]
	result := make([]byte, len(continuation))
	copy(result, continuation)

	return result
}

func (spanIndex *SpanIndex) advance(node *spanNode, symbol byte) *spanNode {
	if node.children == nil {
		node.children = map[byte]*spanNode{}
	}

	next, ok := node.children[symbol]
	if !ok {
		next = &spanNode{
			sample: -1,
		}
		node.children[symbol] = next
	}

	return next
}

func (spanIndex *SpanIndex) observe(
	node *spanNode,
	sampleIndex int,
	suffixFrom int,
) {
	if node == nil || spanIndex.better(node, sampleIndex, suffixFrom) {
		return
	}

	node.sample = sampleIndex
	node.suffixFrom = suffixFrom
}

func (spanIndex *SpanIndex) better(
	node *spanNode,
	sampleIndex int,
	suffixFrom int,
) bool {
	if node.sample < 0 {
		return false
	}

	current := spanIndex.samples[node.sample]
	candidate := spanIndex.samples[sampleIndex]

	currentRemaining := len(current) - node.suffixFrom
	candidateRemaining := len(candidate) - suffixFrom

	if candidateRemaining > currentRemaining {
		return false
	}

	if candidateRemaining < currentRemaining {
		return true
	}

	return sampleIndex >= node.sample
}
