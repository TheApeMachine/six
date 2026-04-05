package frankentrie

import "sort"

func sortedChildTokens(node *Node) []string {
	tokens := make([]string, 0, len(node.Children))
	for token := range node.Children {
		tokens = append(tokens, token)
	}

	sort.Strings(tokens)
	return tokens
}

func beamsClosed(beams []beamState, endToken string) bool {
	for _, beam := range beams {
		if len(beam.Tokens) == 0 {
			return false
		}

		if beam.Tokens[len(beam.Tokens)-1] != endToken {
			return false
		}
	}

	return true
}

func subtreeSize(node *Node) int {
	size := 1
	for _, child := range node.Children {
		size += subtreeSize(child)
	}

	return size
}
