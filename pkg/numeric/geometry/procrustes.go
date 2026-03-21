package geometry

import (
	"fmt"
	"math"
)

/*
ProcrustesResult holds the outcome of an orthogonal Procrustes alignment
between two embedding spaces. R is the nDim×nDim rotation matrix that
minimizes ||R·A − B||_F², and Residual is the squared Frobenius norm of
the post-alignment error.
*/
type ProcrustesResult struct {
	R        [][]float64
	Residual float64
}

/*
Procrustes computes the orthogonal Procrustes alignment between matrices
A and B (both nSamples × nDim). Finds the rotation R minimizing
||R·A − B||² via SVD of M = Bᵀ·A, then R = U·Vᵀ. A sign correction on
the last column of U enforces det(R) = +1 (proper rotation).

Uses a hand-rolled Jacobi-rotation SVD so the solver carries zero
external linear-algebra dependencies.
*/
func Procrustes(matA, matB [][]float64, nSamples, nDim int) (*ProcrustesResult, error) {
	if nSamples < 1 || nDim < 1 {
		return nil, ProcrustesError("degenerate dimensions")
	}

	if len(matA) != nSamples || len(matB) != nSamples {
		return nil, ProcrustesError("row count mismatch")
	}

	crossProduct := make([][]float64, nDim)
	for row := range crossProduct {
		crossProduct[row] = make([]float64, nDim)
	}

	for sample := 0; sample < nSamples; sample++ {
		if len(matA[sample]) != nDim || len(matB[sample]) != nDim {
			return nil, ProcrustesError("column count mismatch")
		}

		for row := 0; row < nDim; row++ {
			for col := 0; col < nDim; col++ {
				crossProduct[row][col] += matB[sample][row] * matA[sample][col]
			}
		}
	}

	uMat, _, vMat, svdErr := JacobiSVD(crossProduct, nDim, nDim)
	if svdErr != nil {
		return nil, svdErr
	}

	rotation := matMul(uMat, transpose(vMat, nDim, nDim), nDim, nDim, nDim)

	if determinant(rotation, nDim) < 0 {
		for row := 0; row < nDim; row++ {
			uMat[row][nDim-1] = -uMat[row][nDim-1]
		}

		rotation = matMul(uMat, transpose(vMat, nDim, nDim), nDim, nDim, nDim)
	}

	var residual float64

	for sample := 0; sample < nSamples; sample++ {
		for dim := 0; dim < nDim; dim++ {
			var rotated float64

			for inner := 0; inner < nDim; inner++ {
				rotated += rotation[dim][inner] * matA[sample][inner]
			}

			diff := rotated - matB[sample][dim]
			residual += diff * diff
		}
	}

	return &ProcrustesResult{R: rotation, Residual: residual}, nil
}

/*
JacobiSVD computes the thin SVD of an m×n matrix (m ≥ n) using cyclic
Jacobi rotations on AᵀA. Returns U (m×n), singular values Σ (length n),
and V (n×n). Convergence criterion: the largest off-diagonal element of
the Gram matrix drops below ε·‖diag‖.

The method repeatedly applies 2×2 Jacobi rotations to pairs (p,q) with
p < q, zeroing the (p,q) element of AᵀA while accumulating the rotation
into V. After convergence the singular values are the column norms of the
updated A, and U is A with each column divided by its singular value.
*/
func JacobiSVD(matrix [][]float64, rows, cols int) ([][]float64, []float64, [][]float64, error) {
	if rows < cols {
		return nil, nil, nil, ProcrustesError(fmt.Sprintf(
			"JacobiSVD requires rows ≥ cols, got %d × %d", rows, cols,
		))
	}

	work := make([][]float64, rows)
	for row := range work {
		work[row] = make([]float64, cols)
		copy(work[row], matrix[row])
	}

	vMat := eye(cols)

	const maxSweeps = 100
	tolerance := 1e-12

	for sweep := 0; sweep < maxSweeps; sweep++ {
		converged := true

		for p := 0; p < cols-1; p++ {
			for q := p + 1; q < cols; q++ {
				var alpha, beta, gamma float64

				for row := 0; row < rows; row++ {
					alpha += work[row][p] * work[row][p]
					beta += work[row][q] * work[row][q]
					gamma += work[row][p] * work[row][q]
				}

				if math.Abs(gamma) < tolerance*(math.Sqrt(alpha*beta)+tolerance) {
					continue
				}

				converged = false
				zeta := (beta - alpha) / (2.0 * gamma)
				tangent := math.Copysign(1.0, zeta) / (math.Abs(zeta) + math.Sqrt(1.0+zeta*zeta))
				cosine := 1.0 / math.Sqrt(1.0+tangent*tangent)
				sine := tangent * cosine

				for row := 0; row < rows; row++ {
					wp := work[row][p]
					wq := work[row][q]
					work[row][p] = cosine*wp - sine*wq
					work[row][q] = sine*wp + cosine*wq
				}

				for row := 0; row < cols; row++ {
					vp := vMat[row][p]
					vq := vMat[row][q]
					vMat[row][p] = cosine*vp - sine*vq
					vMat[row][q] = sine*vp + cosine*vq
				}
			}
		}

		if converged {
			break
		}
	}

	sigma := make([]float64, cols)
	uMat := make([][]float64, rows)
	for row := range uMat {
		uMat[row] = make([]float64, cols)
	}

	for col := 0; col < cols; col++ {
		var norm float64

		for row := 0; row < rows; row++ {
			norm += work[row][col] * work[row][col]
		}

		sigma[col] = math.Sqrt(norm)

		if sigma[col] > tolerance {
			for row := 0; row < rows; row++ {
				uMat[row][col] = work[row][col] / sigma[col]
			}
		}
	}

	return uMat, sigma, vMat, nil
}

/*
matMul multiplies two dense matrices a (rA × cA) and b (cA × cB),
returning the rA × cB product.
*/
func matMul(a, b [][]float64, rA, cA, cB int) [][]float64 {
	out := make([][]float64, rA)

	for row := range out {
		out[row] = make([]float64, cB)

		for col := 0; col < cB; col++ {
			var sum float64

			for inner := 0; inner < cA; inner++ {
				sum += a[row][inner] * b[inner][col]
			}

			out[row][col] = sum
		}
	}

	return out
}

/*
transpose returns the n×m transpose of an m×n matrix.
*/
func transpose(mat [][]float64, rows, cols int) [][]float64 {
	out := make([][]float64, cols)

	for col := range out {
		out[col] = make([]float64, rows)

		for row := 0; row < rows; row++ {
			out[col][row] = mat[row][col]
		}
	}

	return out
}

/*
eye returns the n×n identity matrix.
*/
func eye(n int) [][]float64 {
	mat := make([][]float64, n)

	for row := range mat {
		mat[row] = make([]float64, n)
		mat[row][row] = 1.0
	}

	return mat
}

/*
determinant computes the determinant of an n×n matrix via LU decomposition
with partial pivoting. Used once per Procrustes call to verify reflection
parity.
*/
func determinant(mat [][]float64, n int) float64 {
	lu := make([][]float64, n)
	for row := range lu {
		lu[row] = make([]float64, n)
		copy(lu[row], mat[row])
	}

	sign := 1.0

	for col := 0; col < n; col++ {
		pivotRow := col
		pivotVal := math.Abs(lu[col][col])

		for row := col + 1; row < n; row++ {
			if math.Abs(lu[row][col]) > pivotVal {
				pivotRow = row
				pivotVal = math.Abs(lu[row][col])
			}
		}

		if pivotRow != col {
			lu[col], lu[pivotRow] = lu[pivotRow], lu[col]
			sign = -sign
		}

		if lu[col][col] == 0 {
			return 0
		}

		for row := col + 1; row < n; row++ {
			factor := lu[row][col] / lu[col][col]

			for inner := col + 1; inner < n; inner++ {
				lu[row][inner] -= factor * lu[col][inner]
			}
		}
	}

	det := sign

	for diag := 0; diag < n; diag++ {
		det *= lu[diag][diag]
	}

	return det
}

/*
ProcrustesError is a typed error for Procrustes and SVD failures.
*/
type ProcrustesError string

/*
Error implements the error interface for ProcrustesError.
*/
func (err ProcrustesError) Error() string {
	return string(err)
}
