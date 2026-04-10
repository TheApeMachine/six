package programmer

type CPUCompiler struct {
	tokens []Token
	frames []Frame
}

func NewCPUCompiler(tokens []Token) *CPUCompiler {
	return &CPUCompiler{tokens: tokens}
}

func (compiler *CPUCompiler) Compile() ([]Frame, error) {
	return nil, nil
}
