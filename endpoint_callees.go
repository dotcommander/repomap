package repomap

// handlerCallees returns the direct (depth-1) callee names lexically present in
// the handler body spanning [startLine, endLine] of the Go file at path.
// Phase A stub: no callees (the bounded AST walk lands in Phase C).
func handlerCallees(path string, startLine, endLine int) ([]string, error) {
	return nil, nil
}
