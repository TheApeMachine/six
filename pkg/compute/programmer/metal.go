package programmer

type MetalCompiler struct {
	tokens []Token
	frames []Frame
}

func NewMetalCompiler(tokens []Token) *MetalCompiler {
	return &MetalCompiler{tokens: tokens}
}

func (compiler *MetalCompiler) Compile() ([]Frame, error) {
	return nil, nil
}
