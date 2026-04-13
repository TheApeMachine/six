package primitive

import "github.com/theapemachine/six/pkg/compute/kernel"

/*
StampGoldClassificationClass sets Properties word 0 for discrete supervised
classes. Dataset-scale ingest should use vm.Tokenizer.AdoptLabeledSample so
stamping stays on the same path as NewValue minting; this helper exists for
narrow host wiring or tests when no tokenizer batch is involved.
*/
func StampGoldClassificationClass(value *Value, classIdx int) {
	if value == nil {
		return
	}

	value.Set(kernel.PropertiesStartWord, kernel.GoldLabelWord(classIdx))
}
