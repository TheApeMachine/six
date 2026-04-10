package programmer

type CUDACompiler struct {
	tokens []Token
	frames []Frame
}

func NewCUDACompiler(tokens []Token) *CUDACompiler {
	return &CUDACompiler{tokens: tokens}
}

func (compiler *CUDACompiler) Compile() ([]Frame, error) {
	return nil, nil
}
