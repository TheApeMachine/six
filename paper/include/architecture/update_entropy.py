import re

with open('/Users/theapemachine/go/src/github.com/theapemachine/six/paper/include/architecture/entropy_routing.tex', 'r') as f:
    c = f.read()

c = c.replace(
    'We confine the atomic semantic chord to a highly compressed 512-bit vector, but geometrically map 27 such chords into a $3 \\times 3 \\times 3$ nested $SO(3)$ manifold (the \\textit{MacroCube}).',
    'We confine the atomic semantic chord to a highly compressed 257-bit vector, but geometrically map 257 such chords into a nested manifold (the \\textit{MacroCube}), addressed by $GF(257)$ affine transformations.'
)
c = c.replace(
    'Before the local micro-block crosses the entropy peak and loses discriminative capacity, the system triggers a discrete Lie group rotation (e.g., a $90^\\circ$ transposition along the X, Y, or Z axis). Because 3D rotations are non-commutative ($R_x R_y \\neq R_y R_x$), this physical manipulation achieves two simultaneous topological functions:',
    'Before the local micro-block crosses the entropy peak and loses discriminative capacity, the system triggers an affine transformation over the finite field $GF(257)$. Because affine transformations $f(p) = (ap + b) \\pmod{257}$ are non-commutative, this mathematical manipulation achieves two simultaneous topological functions:'
)
c = c.replace('The 512-Bit Chord', 'The 257-Bit Chord')
c = c.replace('$D = 512$', '$D = 257$')
c = c.replace('$2^{512} \\approx 1.34 \\times 10^{154}$', '$2^{257} \\approx 2.31 \\times 10^{77}$')

c = c.replace('Level 2: The MacroCube ($3 \\times 3 \\times 3$)', 'Level 2: The MacroCube (257 faces)')
c = c.replace('Twenty-seven chords are arranged in a rigid lattice', '257 chords are arranged in a rigid ring lattice')
c = c.replace('|\\Omega_{\\text{cube}}| = (2^{512})^{27} = 2^{13{,}824}', '|\\Omega_{\\text{cube}}| = (2^{257})^{257} = 2^{66{,}049}')
c = c.replace('\\approx 10^{4{,}162}', '\\approx 10^{19{,}882}')
c = c.replace('$27 \\times 64 = 1{,}728$ bytes $\\approx 1.7$~KB', '$257 \\times 64 = 16{,}448$ bytes $\\approx 16.4$~KB')

c = c.replace('135 chords', '1285 chords')
c = c.replace('|\\Omega_{\\text{ico}}| = 2^{135 \\times 512} = 2^{69{,}120}', '|\\Omega_{\\text{ico}}| = 2^{1285 \\times 257} = 2^{330{,}245}')
c = c.replace('\\approx 10^{20{,}809}', '\\approx 10^{99{,}413}')
c = c.replace('$135 \\times 64 + 2 = 8{,}642$ bytes $\\approx 8.64$~KB', '$1285 \\times 64 + 2 = 82{,}242$ bytes $\\approx 82$~KB')

c = c.replace('Cubic mode\n(|\\mathbf{O}| = 24), each distinct bit configuration can exist in 24\ngeometrically distinguishable states.', 'Affine mode, each distinct bit configuration can exist in $257 \\times 256 = 65{,}792$\ngeometrically distinguishable states.')
c = c.replace('Cubic mode\n($|\\mathbf{O}| = 24$), each distinct bit configuration can exist in 24\ngeometrically distinguishable states.', 'Affine mode, each distinct bit configuration can exist in $257 \\times 256 = 65{,}792$\ngeometrically distinguishable states.')

c = c.replace('Post-mitosis ($|A_5| = 60$), this\nrises to 60.', 'Post-mitosis with the $A_5$ working primitive, this\nrotational memory space interacts with a 60-state macro-permutation.')

c = c.replace('|\\Omega_{\\text{total}}| = 2^{69{,}120} \\times 60 \\times 16', '|\\Omega_{\\text{total}}| = 2^{330{,}245} \\times 65{,}792 \\times 16')
c = c.replace('= 2^{69{,}120} \\times 960', '= 2^{330{,}245} \\times 1{,}052{,}672')

c = c.replace('Because the $A_5$ rotations are \\emph{non-commutative}', 'Because the $GF(257)$ affine transformations are \\emph{non-commutative}')
c = c.replace('the same final $A_5$ element', 'the same final affine element')

c = c.replace('512-dim float32 embedding', '257-dim float32 embedding')
c = c.replace('Single MacroCube (Cubic)', 'Single MacroCube (Affine)')
c = c.replace('1.7 KB', '16.4 KB')
c = c.replace('2^{13{,}824} \\times 24 \\times 16', '2^{66{,}049} \\times 65{,}792 \\times 16')
c = c.replace('\\sim 2{,}450', '\\sim 1{,}212')

c = c.replace('8.64 KB', '82 KB')
c = c.replace('2^{69{,}120} \\times 60 \\times 16', '2^{330{,}245} \\times 65{,}792 \\times 16')
c = c.replace('\\sim 2{,}408', '\\sim 1{,}211')

c = c.replace('8.6 GB', '82 GB')
c = c.replace('(2^{69{,}120})^{10^6}', '(2^{330{,}245})^{10^6}')

c = c.replace('limit at the micro-state (the 512-bit chord)', 'limit at the micro-state (the 257-bit chord)')

with open('/Users/theapemachine/go/src/github.com/theapemachine/six/paper/include/architecture/entropy_routing.tex', 'w') as f:
    f.write(c)

