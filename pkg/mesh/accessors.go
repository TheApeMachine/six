package mesh

func (field *Field) MetricsSnapshot() *FieldMetrics {
	if field == nil {
		return nil
	}

	return field.metrics.Load()
}
