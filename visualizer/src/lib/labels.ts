/*
Label slot layout mirrors pkg/compute/kernel.PackClassificationLabelSlots
and the README's "Label Packing (w56)" section. The 64-bit labels word
holds four 16-bit slots packed low-to-high:

  bits 0..15   slot 0 — dataset label, written by the tokenizer at mint
  bits 16..31  slot 1 — unsupervised soft label, round 1
  bits 32..47  slot 2 — unsupervised soft label, round 2
  bits 48..63  slot 3 — unsupervised soft label, round 3

Slot 0 is the ground-truth label the data provider supplied; slots 1–3
are written by the unsupervised learner in successive rounds. Zero in
any slot means "unlabeled in this slot" — it is a sentinel, not a real
label.
*/

const SLOT_NAMES = ["ds", "ul1", "ul2", "ul3"] as const;
const SLOT_MASK = 0xffffn;

export interface LabelSlots {
	dataset: number;
	ul1: number;
	ul2: number;
	ul3: number;
}

export function unpackLabelWord(word: bigint): LabelSlots {
	return {
		dataset: Number(word & SLOT_MASK),
		ul1: Number((word >> 16n) & SLOT_MASK),
		ul2: Number((word >> 32n) & SLOT_MASK),
		ul3: Number((word >> 48n) & SLOT_MASK),
	};
}

/*
formatLabelsWord renders the four slots compactly, dropping any slot
that holds the zero sentinel so an unlabeled or partially-labeled
Value reads as "ds:5" rather than "ds:5 ul1:0 ul2:0 ul3:0".
*/
export function formatLabelsWord(word: bigint): string {
	if (word === 0n) {
		return "unlabeled";
	}

	const slots = unpackLabelWord(word);
	const parts: string[] = [];
	const labeledSlots: { readonly name: (typeof SLOT_NAMES)[number]; value: number }[] =
		[
			{ name: "ds", value: slots.dataset },
			{ name: "ul1", value: slots.ul1 },
			{ name: "ul2", value: slots.ul2 },
			{ name: "ul3", value: slots.ul3 },
		];

	for (const slot of labeledSlots) {
		if (slot.value === 0) {
			continue;
		}

		parts.push(`${slot.name}:${slot.value}`);
	}

	return parts.join(" · ");
}
