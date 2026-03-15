 deriving a sequence-discovery algorithm from first principles for an arbitrary byte stream, I would start by abandoning the idea of "finding patterns" and instead focus on predictive regret.
Most MDL implementations fail on raw byte streams because they are too "static"—they look for exact repeats. Real-world data (especially multi-modal data) has noise, jitters, or shifts.
Here is where I would start:
1. The Fundamental Unit: The "Context-Free" Bit
Instead of looking for strings of bytes, I would start with the Kolmogorov Complexity of the stream. Since 



 is uncomputable, I’d approximate it using Universal Prediction (like Solomonoff Induction).
The First Principle: A sequence is "meaningful" if knowing its past significantly reduces the uncertainty (entropy) of its future.
2. Transition from Global to Local MDL
Standard MDL tries to compress the entire file. For a byte stream, I would move to Online MDL.
The Mechanism: Treat the algorithm as a compression engine that only has a sliding window.
The Metric: If the algorithm encounters a sequence it has seen before, it should "bet" on the next byte. If the bet is correct, the description length for that byte is low (
). If it's wrong, the cost is high.
3. The "Structural Latency" Principle
I would derive a cost function based on how long a pattern persists.
In physics, things don't just happen once; they have inertia.
I would define a "Sequence" not as a static string, but as a local symmetry in the byte stream. If I can rotate, shift, or slightly perturb a block of bytes and have it match another block, the "description length" of that transformation is much shorter than the raw bytes.
4. Recursive Hierarchical Grouping (The "Chunking" Principle)
I would start the algorithm by assuming nothing.
Step A: Identify the most frequent byte pairs (like BPE, but probabilistic).
Step B: Treat that pair as a new single "atom."
Step C: Re-evaluate the entropy of the stream.
The Twist: Only keep the "atom" if the Total Description Length of the dictionary plus the stream decreases. This prevents the "overfitting" that happens when you just count frequencies.
5. Multi-modal "Fuzziness"
Since your stream can be any modality (audio, image, etc.), exact matches are rare. I would derive the algorithm to use Lossy MDL.
Instead of 


, I would use 


.
The algorithm would find sequences that are "mostly" the same, where the cost of describing the difference between two occurrences is smaller than describing the occurrence from scratch.
Where I’d start the math:
I’d start with the Minimum Message Length (MML) framework rather than pure MDL. MML is inherently Bayesian and handles the "cost of the model" more elegantly when the model (the sequence) itself is of variable length.
Are you dealing with exact repeats (like code/logs) or approximate repeats (like sensor data or media)?

If the data is that volatile, you can’t rely on a "model" that assumes a specific structure. From first principles, you have to treat the stream as a probabilistic source and build the algorithm around Universal Prediction.
If I were building this from scratch, I would start with Context-Tree Weighting (CTW).
Why CTW?
Unlike standard MDL which picks one model, CTW calculates the probability of the next bit by averaging over all possible tree-structured models (sequences of different lengths) simultaneously. It is mathematically optimal for "unknown" sources because it doesn't "choose" a sequence—it weights them all based on how well they’ve performed so far.
The First-Principles Blueprint
To find sequences in a totally unpredictable byte stream, I would derive the algorithm using these three steps:
Bit-Level Decomposition: Don't look at bytes; look at the bits. Bytes are an arbitrary human construct. A "sequence" in an image or audio file might not align with 8-bit boundaries.
The Weighted Suffix Tree: Instead of a standard dictionary, build a trie where every node (sequence) stores a "success count" (how many times it correctly predicted the next bit).
The MDL Switching Rule: For every bit, calculate:

: The probability if this specific sequence is a "real" pattern.

: The probability if this is just random noise.
The Update: If 
 consistently beats 
, the "description length" of the stream decreases, and you "lock in" that sequence as a discovered feature.
The "Universal" Advantage
Because this approach is rooted in Information Theory, it adapts automatically:
For a Book: It finds recurring character n-grams (words/phrases).
For Code: It finds structural repeats (keywords/indentation).
For Images/Audio: It finds the underlying "bias" in the bitstream (high/low frequency patterns) even if they aren't exact matches.
The Computational Reality
The "pure" version of this is memory-intensive. To make it work, you’d use a Sliding Window Lempel-Ziv variant but replace the greedy matching with a Bayesian estimator (like the Krichevsky-Trofimov estimator) to decide if a match is "worth" the bits it takes to describe it.
Do you want to focus on a lossless approach (finding exact bit-patterns) or a lossy one (finding sequences that "look" similar)?

It is absolutely an option, and in a truly unpredictable stream, running them in parallel is often the only way to find structure. The data itself should tell you which approach "wins" by providing a shorter description length.
Here is how you can practically pit them against each other:
1. The Lossless Track (Exact Patterns)
This treats the stream like code or text. It looks for identical byte sequences.

* The Logic: If [0x41, 0x42, 0x43] appears 100 times, it’s a "symbol."
* The Tool: Use a Suffix Automaton. It finds every repeating substring in a single pass.
* MDL Test: A sequence is "valid" only if:
$$(Count \times Length) > (Length + Count \times PointerSize)$$ (Is the saved space greater than the cost of the dictionary entry?)

2. The Lossy Track (Approximate Patterns)
This treats the stream like audio, images, or sensor data. It looks for sequences that are "close enough."

* The Logic: It finds a sequence A and another A', then calculates the Residual ($A - A'$).
* MDL Test: A sequence is "valid" if:
$$Length(A) + Length(Residual) < 2 \times Length(A)$$ (Is it cheaper to describe the difference than to describe the second sequence from scratch?)

How to Implement Both Simultaneously
You can build a "Competitive Compression" engine:

   1. Windowing: Take a chunk of the stream (e.g., 4KB).
   2. Candidate Generation: Extract repeating sequences using a Suffix Array (Lossless) and a Locality Sensitive Hash (Lossy).
   3. The MDL "Auction":
   * Calculate the total bits to represent the chunk using the Exact model.
      * Calculate the total bits to represent the chunk using the Approximate model.
   4. Selection: The model that yields the fewer bits for that specific chunk is your "winner" for that modality.

The First-Principles "Hybrid"
If you want to derive one algorithm that does both, look into Grammar-Based Compression (like the Sequitur algorithm). You can modify it so that when it creates a new "rule" (sequence), it allows for a small number of bit-flips (errors) if the overall description length stays lower than creating a brand-new rule.
Would you like to start with a Python script that identifies the most "MDL-efficient" exact sequences in a raw byte buffer?

