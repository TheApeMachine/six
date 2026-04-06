package levenshtein

/*
Distance is edit distance between strings as rune sequences (token-graph alignment in the trie).
*/
func Distance(left string, right string) int {
	leftRunes := []rune(left)
	rightRunes := []rune(right)

	matrix := make([][]int, len(leftRunes)+1)

	for rowIndex := range matrix {
		matrix[rowIndex] = make([]int, len(rightRunes)+1)
	}

	for rowIndex := 0; rowIndex <= len(leftRunes); rowIndex++ {
		matrix[rowIndex][0] = rowIndex
	}

	for columnIndex := 0; columnIndex <= len(rightRunes); columnIndex++ {
		matrix[0][columnIndex] = columnIndex
	}

	for rowIndex := 1; rowIndex <= len(leftRunes); rowIndex++ {
		for columnIndex := 1; columnIndex <= len(rightRunes); columnIndex++ {
			cost := 0

			if leftRunes[rowIndex-1] != rightRunes[columnIndex-1] {
				cost = 1
			}

			matrix[rowIndex][columnIndex] = min(
				matrix[rowIndex-1][columnIndex]+1,
				min(matrix[rowIndex][columnIndex-1]+1, matrix[rowIndex-1][columnIndex-1]+cost),
			)
		}
	}

	return matrix[len(leftRunes)][len(rightRunes)]
}
