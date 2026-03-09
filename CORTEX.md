# Cortex: Geometric Programming via Topological Resonance

## Core Concept
The Cortex is a pure, geometric substrate. There is no linguistic processing, no semantic understanding, no numbers, and no conceptual "opcodes." There is only the 257-bit **Chord**. 

A "program" in the Cortex is simply a stable geometric arrangement of cubes. "Execution" is the natural, thermodynamic resolution of bitwise tension across their connected edges via fundamental `AND`, `OR`, and `NOT` operations on these 257-dimensional vectors.

## The Primitive: The 6x4x257 Geometry
Each Node in the graph is represented by a physical 3D Cube.
- **6 Sides**: Orienting the cube in 3D space (Up, Down, Left, Right, Front, Back).
- **4 Z-Rotations**: Each side can be physically twisted in 90-degree increments.
- **24 Contact Patches**: This provides 24 distinct docking ports where a Cube can interface with its sequence of neighbors in the graph.

Every single one of these 24 contact patches has a depth of **257 separate 257-bit chords**.

### The Superpower: The 257th Dimension (Face 256)
Indices `0` through `255` on any contact patch store structural shapes (holographic semantic chords). 
The final index, `256` (the 257th chord), is the **Geometric Gate**. 

It is not an instruction register in the CPU sense. It does not hold strings or enums. It is literally a 257-bit mask that dictates the physical permittivity of that specific docking port.
- An all-`0` chord entirely closes the gate.
- An all-`1` chord wide-opens the gate to absorb any geometry.
- A highly specific, fractured chord mathematically filters exactly which neighboring structures can physically dock and pass through to achieve equilibrium.

## Edges as Topological Bit-Gates
When two Cubes connect, they meet at a specific Side and Z-Rotation. The behavior of that edge is defined purely by applying bitwise math to the 24th-dimension gates (the 257th chord) of the respective mating faces.

There are no "If" statements. Computation is decentralized. The physics engine of the graph simply resolves the resonant states via `data/chord.go`:

1. **Topological Syncing**
   - **Cube A (Side 1, Z-Rot 0)**: Its 257th chord contains a specific 257-bit shape.
   - **Cube B (Side 3, Z-Rot 90)**: Its 257th chord contains a `ChordHole` (a deficit perfectly fitting A's shape).
   - **Behavior**: The engine bridges the mathematical gap. The data on Side 1 physically flows across the edge via bitwise `ChordOR` absorption, filling the deficit in Cube B safely.

2. **Geometric Filtering (The "If" Statement)**
   - To route data, a central Cube exposes two different sides.
   - Side 1 presents a 257th chord that returns an exact `ChordAND` match for incoming "Text" shapes.
   - Side 2 presents a 257th chord for "Numeric" shapes.
   - **Behavior**: As data accumulates, it physically cannot pass the `ChordAND` filter on Side 2, but smoothly `ChordOR`s its way through Side 1. This stable geometry physically sorts memory without executing a single line of boolean logic.

This arrangement locks into a permanent circuit. As long as this geometry holds, it becomes a literal, repeatable "tool" that physically sorts memory based entirely on thermodynamic physics.

### Example: Geometric Extraction (The bAbI Task)
Consider the prompt: `"Sandra is in the garden. Roy is in the Kitchen. Where is Sandra?"`

In a classic LLM, this is processed as linguistic tokens. In the Cortex, this is processed purely as a geometric structure of 257-bit strings:

1. **Intersection (The Core)**: The graph absorbs the three statements as dense 3D chord clusters into separate Cubes. It assesses the geometric tension by calculating `ChordAND` across their data faces. It discovers a massive, dense mathematical intersection (`Intersection = ChordAND(CubeA, ChordAND(CubeB, CubeC))`). This identical shared shape acts as a geometric anchor.
2. **Outlier Shedding**: Punctuation shapes (`[.]`, `[?]`) yield `0` when `ChordAND`ed with the anchor. Having no geometric grip, they naturally decouple and shift to the periphery.
3. **Residue Isolation**: The Cubes calculate the difference between their total mass and the anchor using `ChordHole(CubeA, Intersection)` (visually: `AND NOT`). The remaining distinct bitset is the unique geometric residue (e.g., `Sandra -> garden`).
4. **Deploying the Gate**: The final cluster factors its geometry and is left with an incomplete structure (a deficit corresponding to the missing answer). It takes this precise `ChordHole` bitset and writes it to the **257th chord** (Face 256) of one of its contact patches. It has armed its docking port with a geometric lock.
5. **Resolution via The Gate**: The Cube holding the `Sandra -> garden` residue is pressing against this armed contact patch. The graph physics evaluates `ChordAND(Data, Face256_Lock)`. Because the residue is the *exact* geometric complement to the lock, it passes cleanly through the 257th dimension, filling the deficit via `ChordOR` and achieving thermodynamic equilibrium. Note: not a single word was semantically "understood."

## Open Topics for Discussion Engine
To build out the true physics pipeline for this architecture, we must establish:
- **Programming the Gates**: How exactly does the system intentionally populate a precise `ChordHole` onto the 257th chord of a specific Side/Z-rotation when it detects a deficit? Does it naturally overflow into the 257th index as data dimension limits are hit?
- **Orientation Matching**: When two Cubes collide, does the graph continuously cycle through all 24 `[Side, Z-Rot]` contact patches on both Cubes until it maximizes the `ChordAND` value of their 257th chord gates?
