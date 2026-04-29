import { useMemo } from "react";
import { REGION_SPECS } from "@/lib/valueRegions";

/*
RegionHeatmap renders the 128 64-bit words as a single dense strip. Each
word becomes one cell whose opacity tracks popcount (0 → empty, 64 →
full). Region boundaries (tokens / program / signals / context /
gradient / properties / asset / prev / next / id / affinity) get small
labelled ticks above the strip so the eye can find a region without a
legend.
*/
const HEATMAP_COLUMNS = 128;

interface RegionHeatmapProps {
	words: bigint[];
}

export function RegionHeatmap({ words }: RegionHeatmapProps) {
	const normalizedWords = useMemo(
		() => normalizeWords(words, HEATMAP_COLUMNS),
		[words],
	);

	const gridCols = `repeat(${HEATMAP_COLUMNS},1fr)`;

	return (
		<div>
			<div
				className="mb-1 grid gap-px"
				style={{ gridTemplateColumns: gridCols }}
			>
				{REGION_SPECS.map((spec) => (
					<div
						key={spec.name}
						className="overflow-hidden border-l border-white/15 pl-0.5 text-[8px] text-white/45"
						style={{
							gridColumn: `${spec.startWord + 1} / span ${spec.wordCount}`,
						}}
					>
						{spec.name}
					</div>
				))}
			</div>
			<div className="grid gap-px" style={{ gridTemplateColumns: gridCols }}>
				{normalizedWords.map((word, idx) => {
					const pop = popcount64(word);
					const opacity = pop === 0 ? 0.05 : 0.2 + (pop / 64) * 0.8;
					const wordKey = `w${idx}`;

					return (
						<div
							key={wordKey}
							className="h-3 rounded-[1px] bg-emerald-300"
							style={{ opacity }}
							title={`${wordKey} · pop ${pop}`}
						/>
					);
				})}
			</div>
		</div>
	);
}

function normalizeWords(words: bigint[], expectedLength: number): bigint[] {
	if (words.length === expectedLength) {
		return words;
	}

	console.warn(
		`RegionHeatmap: expected ${expectedLength} words, got ${words.length}; padding or truncating`,
	);

	if (words.length < expectedLength) {
		const pad = Array.from({ length: expectedLength - words.length }, () => 0n);

		return words.concat(pad);
	}

	return words.slice(0, expectedLength);
}

function popcount64(word: bigint): number {
	let count = 0;
	let value = BigInt.asUintN(64, word);

	while (value !== 0n) {
		value &= value - 1n;
		count++;
	}

	return count;
}
