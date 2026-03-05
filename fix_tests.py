import os
import glob

def fix_test(path):
    with open(path, 'r') as f:
        content = f.read()

    # Fix queryCtx = ...
    content = content.replace(
        "var queryCtx geometry.IcosahedralManifold\n\t\t\tfor i := 0; i < 8; i++ {\n\t\t\t\tqueryCtx.Cubes[0][0][i] = A[i]\n\t\t\t}",
        "tempField := store.NewPrimeField()\n\t\t\ttempField.Insert(A)\n\t\t\tvar queryCtx geometry.IcosahedralManifold = tempField.Manifold(0)"
    )

    # In expected_reality_test.go
    content = content.replace(
        "queryCtx.Cubes[0][0] = promptChord",
        "tempField := store.NewPrimeField()\n\t\ttempField.Insert(promptChord)\n\t\tqueryCtx = tempField.Manifold(0)"
    )
    content = content.replace(
        "expectedReality.Cubes[0][0] = expectedChord",
        "tempExp := store.NewPrimeField()\n\t\ttempExp.Insert(expectedChord)\n\t\texpectedReality = tempExp.Manifold(0)"
    )
    content = content.replace(
        "expectedM.Cubes[0][0] = targetE",
        "tempE := store.NewPrimeField()\n\t\ttempE.Insert(targetE)\n\t\texpectedM = tempE.Manifold(0)"
    )

    # In contradiction_test.go
    content = content.replace(
        "queryCtx.Cubes[0][0] = queryChord",
        "tempF := store.NewPrimeField()\n\t\ttempF.Insert(queryChord)\n\t\tqueryCtx = tempF.Manifold(0)"
    )

    # Fix Winner checks that hardcode [0][0]
    content = content.replace(
        "winner1.Cubes[0][0].Has(p)",
        "hasPrime(winner1, p)"
    )
    content = content.replace(
        "winner2.Cubes[0][0].Has(p)",
        "hasPrime(winner2, p)"
    )
    content = content.replace(
        "actual.Cubes[0][0].Has(p)",
        "hasPrime(actual, p)"
    )
    content = content.replace(
        "So(actual.Cubes[0][0].Has(p), ShouldBeTrue)",
        "So(hasPrime(actual, p), ShouldBeTrue)"
    )
    content = content.replace(
        "So(actual.Cubes[0][0].Has(p), ShouldBeFalse)",
        "So(hasPrime(actual, p), ShouldBeFalse)"
    )

    
    if "hasPrime(" in content and "func hasPrime" not in content:
        content += """

func hasPrime(m geometry.IcosahedralManifold, p int) bool {
for c := 0; c < 5; c++ {
for b := 0; b < 27; b++ {
if m.Cubes[c][b].Has(p) {
return true
}
}
}
return false
}
"""
    with open(path, 'w') as f:
        f.write(content)

for f in glob.glob('/Users/theapemachine/go/src/github.com/theapemachine/six/experiment/task/logic/*.go'):
    fix_test(f)
