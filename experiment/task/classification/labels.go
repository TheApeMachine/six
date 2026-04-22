package classification

func normalizeClassificationLabelIndex(label int, labels []string) (int, bool) {
	// If the labels are 1-indexed (like ag_news which is 1, 2, 3, 4)
	// we should check if the max label is len(labels) and min is 1.
	// But since we don't know, we can assume if label == len(labels), it's 1-indexed.
	// Actually, just check if label is 1-indexed first if we know it's ag_news.
	// But let's just do:
	if label > 0 && label <= len(labels) {
		return label - 1, true
	}

	if label >= 0 && label < len(labels) {
		return label, true
	}

	return 0, false
}

