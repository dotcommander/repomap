package cli

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dotcommander/repomap"
)

type mapRenderResult struct {
	Content string `json:"content"`
}

type mapStatusResult struct {
	BuiltAt string `json:"built_at"`
	Stale   bool   `json:"stale"`
	Root    string `json:"root"`
}

type symbolFindResult struct {
	Matches []repomap.SymbolMatch `json:"matches"`
}

type renderParams struct {
	Format string `json:"format"`
}

type symbolFindParams struct {
	Query string `json:"query"`
}

type fileExplainParams struct {
	Path string `json:"path"`
}

type fileContextParams struct {
	Query          string `json:"query"`
	Kind           string `json:"kind"`
	File           string `json:"file"`
	MaxSourceLines int    `json:"max_source_lines"`
}

func (s *serveServer) rpcMapRender(req rawRequest) (any, *rpcErrObj) {
	var p renderParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, rpcErr(errInvalidParams, err.Error())
	}

	var content string
	switch p.Format {
	case "":
		content = s.m.String()
	case "compact":
		content = s.m.StringCompact()
	case "verbose":
		content = s.m.StringVerbose()
	case "detail":
		content = s.m.StringDetail()
	case "lines":
		content = s.m.StringLines()
	case "xml":
		content = s.m.StringXML()
	case "structured":
		data, err := s.m.StructuredJSON()
		if err != nil {
			return nil, rpcErr(errServer, err.Error())
		}
		content = string(data)
	default:
		return nil, rpcErr(errInvalidParams, "unknown format")
	}
	return mapRenderResult{Content: content}, nil
}

func (s *serveServer) rpcMapStatus(req rawRequest) (any, *rpcErrObj) {
	var p struct{}
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, rpcErr(errInvalidParams, err.Error())
	}

	builtAt := ""
	if t := s.m.BuiltAt(); !t.IsZero() {
		builtAt = t.Format(time.RFC3339)
	}
	return mapStatusResult{BuiltAt: builtAt, Stale: s.m.Stale(), Root: s.root}, nil
}

func (s *serveServer) rpcSymbolFind(req rawRequest) (any, *rpcErrObj) {
	var p symbolFindParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, rpcErr(errInvalidParams, err.Error())
	}
	if p.Query == "" {
		return nil, rpcErr(errInvalidParams, "query must not be empty")
	}

	name, kind, file := repomap.ParseFindQuery(p.Query)
	matches := s.m.FindSymbol(name, kind, file)
	if matches == nil {
		matches = []repomap.SymbolMatch{}
	}
	return symbolFindResult{Matches: matches}, nil
}

func (s *serveServer) rpcFileExplain(req rawRequest) (any, *rpcErrObj) {
	var p fileExplainParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, rpcErr(errInvalidParams, err.Error())
	}
	if p.Path == "" {
		return nil, rpcErr(errInvalidParams, "path must not be empty")
	}

	result, err := s.m.Explain(p.Path)
	if err != nil {
		return nil, rpcErr(errServer, err.Error())
	}
	return result, nil
}

func (s *serveServer) rpcFileContext(req rawRequest) (any, *rpcErrObj) {
	var p fileContextParams
	if err := decodeParams(req.Params, &p); err != nil {
		return nil, rpcErr(errInvalidParams, err.Error())
	}
	if p.Query == "" {
		return nil, rpcErr(errInvalidParams, "query must not be empty")
	}

	result, err := s.m.Context(p.Query, repomap.ContextOptions{
		Kind:           p.Kind,
		File:           p.File,
		MaxSourceLines: p.MaxSourceLines,
	})
	if err != nil {
		return nil, rpcErr(errServer, err.Error())
	}
	return result, nil
}

func decodeParams(params json.RawMessage, dst any) error {
	if len(params) == 0 {
		return nil
	}
	if err := json.Unmarshal(params, dst); err != nil {
		return fmt.Errorf("invalid params: %w", err)
	}
	return nil
}

func rpcErr(code int, msg string) *rpcErrObj {
	return &rpcErrObj{Code: code, Message: msg}
}
