package repomap

import (
	"go/ast"
	"strings"
)

// funcSignature builds "(param, param, ...) ReturnType" from a FuncType.
// Uses param names only when the full signature with types exceeds 40 chars;
// otherwise includes types for clarity.
func funcSignature(ft *ast.FuncType) string {
	paramsShort := paramList(ft.Params, false)
	returnStr := returnString(ft.Results)

	short := "(" + paramsShort + ")"
	if returnStr != "" {
		short += " " + returnStr
	}

	if len(short) <= 40 {
		// Try with types — if it still fits, prefer the richer form.
		paramsLong := paramList(ft.Params, true)
		long := "(" + paramsLong + ")"
		if returnStr != "" {
			long += " " + returnStr
		}
		if len(long) <= 40 {
			return long
		}
	}

	return short
}

// paramList formats parameters. If withTypes is true, includes type
// annotations; otherwise emits only param names (or "_" for unnamed).
func paramList(fl *ast.FieldList, withTypes bool) string {
	if fl == nil {
		return ""
	}

	var parts []string
	for _, field := range fl.List {
		typ := typeString(field.Type)
		if len(field.Names) == 0 {
			// Unnamed parameter — emit type or "_".
			if withTypes {
				parts = append(parts, typ)
			} else {
				parts = append(parts, "_")
			}
			continue
		}
		for _, name := range field.Names {
			if withTypes {
				parts = append(parts, name.Name+" "+typ)
			} else {
				parts = append(parts, name.Name)
			}
		}
	}
	return strings.Join(parts, ", ")
}

// returnString formats the return types of a function.
func returnString(fl *ast.FieldList) string {
	if fl == nil || len(fl.List) == 0 {
		return ""
	}

	var parts []string
	for _, field := range fl.List {
		typ := typeString(field.Type)
		if len(field.Names) == 0 {
			parts = append(parts, typ)
		} else {
			for range field.Names {
				parts = append(parts, typ)
			}
		}
	}

	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// typeString converts an AST type expression to a compact string.
func typeString(expr ast.Expr) string {
	if expr == nil {
		return ""
	}
	switch t := expr.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return "*" + typeString(t.X)
	case *ast.SelectorExpr:
		return typeString(t.X) + "." + t.Sel.Name
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + typeString(t.Elt)
		}
		return "[...]" + typeString(t.Elt)
	case *ast.MapType:
		return "map[" + typeString(t.Key) + "]" + typeString(t.Value)
	case *ast.ChanType:
		switch t.Dir {
		case ast.SEND:
			return "chan<- " + typeString(t.Value)
		case ast.RECV:
			return "<-chan " + typeString(t.Value)
		default:
			return "chan " + typeString(t.Value)
		}
	case *ast.Ellipsis:
		return "..." + typeString(t.Elt)
	case *ast.FuncType:
		return "func(" + paramList(t.Params, true) + ")"
	case *ast.InterfaceType:
		return "interface{}"
	case *ast.StructType:
		return "struct{}"
	case *ast.IndexExpr:
		return typeString(t.X) + "[" + typeString(t.Index) + "]"
	case *ast.IndexListExpr:
		var args []string
		for _, idx := range t.Indices {
			args = append(args, typeString(idx))
		}
		return typeString(t.X) + "[" + strings.Join(args, ", ") + "]"
	default:
		return "..."
	}
}

// structFields extracts exported field names from a struct type.
// Returns a compact representation like "{field1, field2, field3}".
func structFields(st *ast.StructType) string {
	if st.Fields == nil {
		return "{}"
	}

	var names []string
	for _, field := range st.Fields.List {
		// Embedded type (no field names)
		if len(field.Names) == 0 {
			if ident, ok := field.Type.(*ast.Ident); ok && isExported(ident.Name) {
				names = append(names, ident.Name)
			}
			continue
		}
		// Named fields
		for _, name := range field.Names {
			if isExported(name.Name) {
				names = append(names, name.Name)
			}
		}
	}

	if len(names) == 0 {
		return "{}"
	}
	return "{" + strings.Join(names, ", ") + "}"
}

// interfaceMethods extracts method names from an interface type.
// Returns a compact representation like "{Method1, Method2}".
func interfaceMethods(it *ast.InterfaceType) string {
	if it.Methods == nil {
		return "{}"
	}

	var names []string
	for _, field := range it.Methods.List {
		// Embedded interface
		if len(field.Names) == 0 {
			if ident, ok := field.Type.(*ast.Ident); ok && isExported(ident.Name) {
				names = append(names, ident.Name)
			}
			continue
		}
		// Methods
		for _, name := range field.Names {
			if isExported(name.Name) {
				names = append(names, name.Name)
			}
		}
	}

	if len(names) == 0 {
		return "{}"
	}
	return "{" + strings.Join(names, ", ") + "}"
}
