package repomap

import (
	"slices"
	"strings"
)

// DetectImplementations scans parsed files and populates Symbol.Implements
// on struct types whose exported method sets satisfy an interface declared
// in the same project.
//
// Matching is by exported method name only — a struct implements an
// interface if its methods are a superset of the interface's method names.
// This is a proxy for Go's structural typing; it is tight enough for LLM
// signal but not a full type checker (signature compatibility is not
// verified, and embedded interfaces are treated as opaque method names).
func DetectImplementations(files []*FileSymbols) {
	if len(files) == 0 {
		return
	}

	interfaces := collectInterfaces(files)
	if len(interfaces) == 0 {
		return
	}

	typeMethods := collectTypeMethods(files)
	if len(typeMethods) == 0 {
		return
	}

	for fi := range files {
		f := files[fi]
		for si := range f.Symbols {
			s := &f.Symbols[si]
			if s.Kind != "struct" {
				continue
			}
			methods, has := typeMethods[s.Name]
			if !has {
				continue
			}
			var impl []string
			for name, required := range interfaces {
				if name == s.Name {
					continue
				}
				if isMethodSubset(required, methods) {
					impl = append(impl, name)
				}
			}
			if len(impl) > 0 {
				slices.Sort(impl)
				s.Implements = impl
			}
		}
	}
}

// collectInterfaces builds a map of interface name → required exported method names.
// Skips empty interfaces and any interface sharing its name with a struct
// (ambiguous; avoid self-match noise).
func collectInterfaces(files []*FileSymbols) map[string][]string {
	ifaces := make(map[string][]string)
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, s := range f.Symbols {
			if s.Kind != "interface" {
				continue
			}
			methods := parseMemberList(s.Signature)
			if len(methods) == 0 {
				continue
			}
			ifaces[s.Name] = methods
		}
	}
	return ifaces
}

// collectTypeMethods maps receiver type name → set of exported method names.
// Strips leading '*' from receivers so value and pointer receivers unify.
func collectTypeMethods(files []*FileSymbols) map[string]map[string]bool {
	m := make(map[string]map[string]bool)
	for _, f := range files {
		if f == nil {
			continue
		}
		for _, s := range f.Symbols {
			if s.Kind != "method" || s.Receiver == "" || !s.Exported {
				continue
			}
			recv := strings.TrimPrefix(s.Receiver, "*")
			if m[recv] == nil {
				m[recv] = make(map[string]bool)
			}
			m[recv][s.Name] = true
		}
	}
	return m
}

// parseMemberList parses a signature like "{A, B, C}" into ["A", "B", "C"].
// Returns nil for empty or malformed input.
func parseMemberList(sig string) []string {
	if len(sig) < 2 || sig[0] != '{' || sig[len(sig)-1] != '}' {
		return nil
	}
	inner := strings.TrimSpace(sig[1 : len(sig)-1])
	if inner == "" {
		return nil
	}
	parts := strings.Split(inner, ",")
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		if n := strings.TrimSpace(p); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// isMethodSubset reports whether all required method names are present in have.
func isMethodSubset(required []string, have map[string]bool) bool {
	for _, name := range required {
		if !have[name] {
			return false
		}
	}
	return true
}
