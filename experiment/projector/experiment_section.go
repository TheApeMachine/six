package projector

// ExperimentSectionTmpl is the canonical Go text/template for rendering a
// standardised experiment subsection.  The template expects an
// ExperimentSection struct (defined in the experiment package) as its
// data context.
const ExperimentSectionTmpl = `\subsection{ {{- .Title -}} }
\label{sec:{{.Label}}}

\paragraph{Task Description.}
{{.TaskDescription}}

\paragraph{Results.}
{{.Results}}
{{- if .Assessment}}

\paragraph{Assessment.}
{{.Assessment}}
{{- end}}
{{- if .Table}}

{{.Table}}
{{- end}}
{{- if .FigureRef}}

Figure~\ref{ {{- .FigureRef -}}} shows the trial outcome map.
{{- end}}
`
