package classification

func normalizeClassificationLabelIndex(label int, labels []string) (int, bool) {
	if label >= 0 && label < len(labels) {
		return label, true
	}

	if label > 0 && label <= len(labels) {
		return label - 1, true
	}

	return 0, false
}
