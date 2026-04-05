package markovtrie

import (
	"math"
	"sort"
	"strings"
	"unicode/utf8"
)

/*
LearnBytePairEncoder trains merge ranks from whitespace and underscore
delimited words extracted out of corpus lines.
*/
func LearnBytePairEncoder(corpus []string, mergeCount int) *BytePairEncoder {
	wordFrequency := make(map[string]int)

	for _, line := range corpus {
		for _, word := range splitWordsFromLine(line) {
			if word == "" {
				continue
			}

			wordFrequency[word]++
		}
	}

	type splitWord struct {
		parts []string
		count int
	}

	vocabulary := make([]splitWord, 0, len(wordFrequency))
	for word, frequency := range wordFrequency {
		wordRunes := []rune(word)
		parts := make([]string, 0, len(wordRunes)+1)

		for _, value := range wordRunes {
			parts = append(parts, string(value))
		}

		parts = append(parts, bpeEndOfWordToken)
		vocabulary = append(vocabulary, splitWord{
			parts: parts,
			count: frequency,
		})
	}

	mergeRank := make(map[string]int)

	for mergeIndex := 0; mergeIndex < mergeCount; mergeIndex++ {
		pairTotals := make(map[string]int)

		for _, word := range vocabulary {
			for partIndex := 0; partIndex < len(word.parts)-1; partIndex++ {
				pairKey := word.parts[partIndex] + bpePairDelimiter + word.parts[partIndex+1]
				pairTotals[pairKey] += word.count
			}
		}

		pairKeys := make([]string, 0, len(pairTotals))
		for pairKey := range pairTotals {
			pairKeys = append(pairKeys, pairKey)
		}

		sort.Slice(pairKeys, func(i, j int) bool {
			ci := pairTotals[pairKeys[i]]
			cj := pairTotals[pairKeys[j]]
			if ci != cj {
				return ci > cj
			}

			return pairKeys[i] < pairKeys[j]
		})

		bestPair := ""
		bestCount := 0

		if len(pairKeys) > 0 {
			bestPair = pairKeys[0]
			bestCount = pairTotals[bestPair]
		}

		if bestCount == 0 {
			break
		}

		left, right, found := strings.Cut(bestPair, bpePairDelimiter)
		if !found || left == "" || right == "" {
			break
		}

		for wordIndex := range vocabulary {
			word := &vocabulary[wordIndex]
			mergedParts := make([]string, 0, len(word.parts))

			for partIndex := 0; partIndex < len(word.parts); {
				if partIndex < len(word.parts)-1 && word.parts[partIndex] == left && word.parts[partIndex+1] == right {
					mergedParts = append(mergedParts, left+right)
					partIndex += 2

					continue
				}

				mergedParts = append(mergedParts, word.parts[partIndex])
				partIndex++
			}

			word.parts = mergedParts
		}

		mergeRank[bestPair] = mergeIndex
	}

	return &BytePairEncoder{
		mergeRank: mergeRank,
	}
}

func splitWordsFromLine(line string) []string {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	return strings.FieldsFunc(line, func(value rune) bool {
		return value == '_' || value == ' ' || value == '\t' || value == '\n' || value == '\r'
	})
}

/*
encodeWord applies learned merges until no adjacent pair exists in mergeRank.
*/
func (encoder *BytePairEncoder) encodeWord(word string) []string {
	if encoder == nil || len(encoder.mergeRank) == 0 {
		runes := []rune(word)
		out := make([]string, len(runes))
		for i, value := range runes {
			out[i] = string(value)
		}

		return out
	}

	parts := make([]string, 0, utf8.RuneCountInString(word)+1)

	for _, value := range []rune(word) {
		parts = append(parts, string(value))
	}

	parts = append(parts, bpeEndOfWordToken)

	for len(parts) > 1 {
		bestRank := math.MaxInt32
		bestIndex := -1

		for partIndex := 0; partIndex < len(parts)-1; partIndex++ {
			pairKey := parts[partIndex] + bpePairDelimiter + parts[partIndex+1]
			rank, exists := encoder.mergeRank[pairKey]
			if !exists || rank >= bestRank {
				continue
			}

			bestRank = rank
			bestIndex = partIndex
		}

		if bestIndex < 0 {
			break
		}

		merged := parts[bestIndex] + parts[bestIndex+1]
		parts = append(parts[:bestIndex], append([]string{merged}, parts[bestIndex+2:]...)...)
	}

	return parts
}

/*
EncodeDocument tokenizes a full line with underscore boundaries preserved as
literal separator tokens between word groups.
*/
func (encoder *BytePairEncoder) EncodeDocument(text string) []string {
	if encoder == nil {
		return splitWordsFromLine(text)
	}

	words := splitWordsFromLine(text)
	if len(words) == 0 {
		return nil
	}

	tokens := make([]string, 0, len(words)*4)

	for wordIndex, word := range words {
		if wordIndex > 0 {
			tokens = append(tokens, "_")
		}

		tokens = append(tokens, encoder.encodeWord(word)...)
	}

	return tokens
}

/*
Tokenize splits text into content and separator tokens while preserving
underscore and space runs as standalone tokens, unless a byte-pair encoder is
attached in which case subword tokens mirror EncodeDocument output.
*/
func (store *Store) Tokenize(text string) []string {
	if store == nil {
		return nil
	}

	store.mu.Lock()
	defer store.mu.Unlock()

	return store.tokenizeUnlocked(text)
}

/*
tokenizeUnlocked is the Tokenize implementation; the caller must hold store.mu.
*/
func (store *Store) tokenizeUnlocked(text string) []string {
	if store.bpe != nil {
		tokens := store.bpe.EncodeDocument(text)
		if len(tokens) == 0 {
			return nil
		}

		return tokens
	}

	if store.wordTokensOnly {
		return splitWordsFromLine(text)
	}

	if text == "" {
		return nil
	}

	tokens := make([]string, 0, len(text))
	tokenStart := 0
	firstRune, _ := utf8.DecodeRuneInString(text)
	separatorMode := isSeparatorRune(firstRune)

	for byteIndex, value := range text {
		currentSeparator := isSeparatorRune(value)
		if byteIndex == 0 {
			separatorMode = currentSeparator
			continue
		}

		if currentSeparator == separatorMode {
			continue
		}

		tokens = append(tokens, text[tokenStart:byteIndex])
		tokenStart = byteIndex
		separatorMode = currentSeparator
	}

	tokens = append(tokens, text[tokenStart:])

	return tokens
}
