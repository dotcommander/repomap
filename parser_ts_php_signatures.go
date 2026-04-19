//go:build !notreesitter

package repomap

import (
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// modifierKinds is the set of tree-sitter node kinds that represent PHP
// visibility and behavior modifiers. They appear as direct children of
// method_declaration, property_declaration, and const_declaration in source order.
var modifierKinds = map[string]bool{
	"visibility_modifier": true,
	"static_modifier":     true,
	"abstract_modifier":   true,
	"final_modifier":      true,
	"readonly_modifier":   true,
	"var_modifier":        true,
}

// phpCollectModifiers walks the direct children of node and returns modifier
// text joined by spaces, in source order. Also returns the visibility text
// separately so callers can determine exported status without re-parsing.
func phpCollectModifiers(node *tree_sitter.Node, source []byte) (modifiers, visibility string) {
	var parts []string
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		k := child.Kind()
		if !modifierKinds[k] {
			continue
		}
		text := child.Utf8Text(source)
		parts = append(parts, text)
		if k == "visibility_modifier" {
			visibility = text
		}
	}
	return strings.Join(parts, " "), visibility
}

// phpVisibilityToExported maps PHP visibility to the Exported flag.
// Default (empty visibility) is public in PHP.
func phpVisibilityToExported(visibility string) bool {
	return visibility == "public" || visibility == ""
}

// phpClassSignature builds: [modifiers] class Name [extends Base] [implements I1, I2].
// Abstract/final modifiers and base/interface clauses are all unnamed children —
// they have no field name, so we walk by kind.
func phpClassSignature(node *tree_sitter.Node, source []byte) string {
	var sb strings.Builder

	var modParts []string
	var nameText string
	var baseText string
	var ifaceText string

	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "abstract_modifier", "final_modifier":
			modParts = append(modParts, child.Utf8Text(source))
		case "name":
			nameText = child.Utf8Text(source)
		case "base_clause":
			// Text is already "extends ClassName".
			baseText = child.Utf8Text(source)
		case "class_interface_clause":
			// Text is already "implements I1, I2".
			ifaceText = child.Utf8Text(source)
		}
	}

	if len(modParts) > 0 {
		sb.WriteString(strings.Join(modParts, " "))
		sb.WriteByte(' ')
	}
	sb.WriteString("class ")
	sb.WriteString(nameText)
	if baseText != "" {
		sb.WriteByte(' ')
		sb.WriteString(baseText)
	}
	if ifaceText != "" {
		sb.WriteByte(' ')
		sb.WriteString(ifaceText)
	}

	return sb.String()
}

// phpMethodSignature builds: [modifiers] function name(params)[: return].
// Delegates to phpFunctionLikeSignature with the modifier prefix prepended.
func phpMethodSignature(node *tree_sitter.Node, source []byte) string {
	mods, _ := phpCollectModifiers(node, source)
	nameNode := node.ChildByFieldName("name")
	if nameNode == nil {
		return ""
	}
	prefix := "function "
	if mods != "" {
		prefix = mods + " function "
	}
	return phpFunctionLikeSignature(prefix, nameNode,
		node.ChildByFieldName("parameters"),
		node.ChildByFieldName("return_type"),
		source)
}

// phpPropertySignature builds: [modifiers] [type] $name [= value].
func phpPropertySignature(node *tree_sitter.Node, source []byte) string {
	mods, _ := phpCollectModifiers(node, source)

	typeNode := node.ChildByFieldName("type")

	// property_element is a named child (not a field); find it by kind.
	var propElement *tree_sitter.Node
	for i := range node.NamedChildCount() {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "property_element" {
			propElement = child
			break
		}
	}
	if propElement == nil {
		return ""
	}

	nameNode := propElement.ChildByFieldName("name")
	defaultNode := propElement.ChildByFieldName("default_value")

	if nameNode == nil {
		return ""
	}

	var sb strings.Builder
	if mods != "" {
		sb.WriteString(mods)
		sb.WriteByte(' ')
	}
	if typeNode != nil {
		sb.WriteString(typeNode.Utf8Text(source))
		sb.WriteByte(' ')
	}
	// variable_name node includes the $ prefix already.
	sb.WriteString(nameNode.Utf8Text(source))

	if defaultNode != nil {
		sb.WriteString(" = ")
		sb.WriteString(strings.TrimSpace(defaultNode.Utf8Text(source)))
	}

	return sb.String()
}

// phpConstSignature builds: [modifiers] const [type] NAME = value.
func phpConstSignature(node *tree_sitter.Node, source []byte) string {
	mods, _ := phpCollectModifiers(node, source)

	// const_declaration may have an optional type child (PHP 8.3+).
	typeNode := node.ChildByFieldName("type")

	// const_element is a named child containing the name and value.
	var constElement *tree_sitter.Node
	for i := range node.NamedChildCount() {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == "const_element" {
			constElement = child
			break
		}
	}
	if constElement == nil {
		return ""
	}

	// const_element's first named child is the name, second is the value.
	var nameNode, valNode *tree_sitter.Node
	if constElement.NamedChildCount() >= 1 {
		nameNode = constElement.NamedChild(0)
	}
	if constElement.NamedChildCount() >= 2 {
		valNode = constElement.NamedChild(1)
	}
	if nameNode == nil {
		return ""
	}

	var sb strings.Builder
	if mods != "" {
		sb.WriteString(mods)
		sb.WriteByte(' ')
	}
	sb.WriteString("const ")
	if typeNode != nil {
		sb.WriteString(typeNode.Utf8Text(source))
		sb.WriteByte(' ')
	}
	sb.WriteString(nameNode.Utf8Text(source))
	if valNode != nil {
		sb.WriteString(" = ")
		sb.WriteString(strings.TrimSpace(valNode.Utf8Text(source)))
	}

	return sb.String()
}

// phpFunctionLikeSignature builds the shared params+return portion reused by
// both free functions and methods. prefix should already contain modifiers and
// the "function" keyword. nameNode is the function/method name node.
func phpFunctionLikeSignature(prefix string, nameNode, paramsNode, returnNode *tree_sitter.Node, source []byte) string {
	var sb strings.Builder
	sb.WriteString(prefix)
	if nameNode != nil {
		sb.WriteString(nameNode.Utf8Text(source))
	}
	if paramsNode != nil {
		sb.WriteString(paramsNode.Utf8Text(source))
	} else {
		sb.WriteString("()")
	}
	if returnNode != nil {
		sb.WriteString(": ")
		sb.WriteString(returnNode.Utf8Text(source))
	}
	return sb.String()
}

// phpFunctionSignature builds: function name(params)[: return].
// Reuses phpFunctionLikeSignature — same shape as method, no modifiers.
func phpFunctionSignature(node *tree_sitter.Node, source []byte) string {
	nameNode := node.ChildByFieldName("name")
	paramsNode := node.ChildByFieldName("parameters")
	returnNode := node.ChildByFieldName("return_type")
	return phpFunctionLikeSignature("function ", nameNode, paramsNode, returnNode, source)
}

// phpInterfaceSignature builds: interface Name [extends I1, I2].
// The base clause is an unnamed child — walk by kind.
func phpInterfaceSignature(node *tree_sitter.Node, source []byte) string {
	var nameText, baseText string
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "name":
			nameText = child.Utf8Text(source)
		case "base_clause":
			// Text is "extends I1, I2".
			baseText = child.Utf8Text(source)
		}
	}
	var sb strings.Builder
	sb.WriteString("interface ")
	sb.WriteString(nameText)
	if baseText != "" {
		sb.WriteByte(' ')
		sb.WriteString(baseText)
	}
	return sb.String()
}

// phpTraitSignature builds: trait Name.
func phpTraitSignature(node *tree_sitter.Node, source []byte) string {
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return "trait " + nameNode.Utf8Text(source)
	}
	// Fall back to walking children by kind.
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child != nil && child.Kind() == "name" {
			return "trait " + child.Utf8Text(source)
		}
	}
	return ""
}

// phpEnumSignature builds: enum Name[: backing] [implements I1, I2].
// primitive_type (backing) and class_interface_clause are unnamed children —
// walk by kind, same pattern as phpClassSignature.
func phpEnumSignature(node *tree_sitter.Node, source []byte) string {
	var nameText, backingText, ifaceText string
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "name":
			nameText = child.Utf8Text(source)
		case "primitive_type":
			backingText = child.Utf8Text(source)
		case "class_interface_clause":
			ifaceText = child.Utf8Text(source)
		}
	}
	var sb strings.Builder
	sb.WriteString("enum ")
	sb.WriteString(nameText)
	if backingText != "" {
		sb.WriteString(": ")
		sb.WriteString(backingText)
	}
	if ifaceText != "" {
		sb.WriteByte(' ')
		sb.WriteString(ifaceText)
	}
	return sb.String()
}

// phpCaseSignature builds: case Name [= value].
// The value node (if present) is grabbed as raw source text.
func phpCaseSignature(node *tree_sitter.Node, source []byte) string {
	var nameText, valText string
	for i := range node.ChildCount() {
		child := node.Child(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "name":
			nameText = child.Utf8Text(source)
		}
	}
	// value is a named field in the grammar.
	if valNode := node.ChildByFieldName("value"); valNode != nil {
		valText = strings.TrimSpace(valNode.Utf8Text(source))
	}
	if valText != "" {
		return "case " + nameText + " = " + valText
	}
	return "case " + nameText
}

// phpPromotedPropertiesFromConstructor extracts property_promotion_parameter
// children from a __construct method node and returns one phpMatchRecord per
// promoted parameter. Signature shape matches phpPropertySignature exactly so
// LLMs cannot distinguish promoted from explicitly-declared properties.
//
// Only parameters with a visibility_modifier become promoted properties — plain
// typed parameters (simple_parameter, variadic_parameter) are skipped.
func phpPromotedPropertiesFromConstructor(methodNode *tree_sitter.Node, receiver string, source []byte) []phpMatchRecord {
	paramsNode := methodNode.ChildByFieldName("parameters")
	if paramsNode == nil {
		return nil
	}

	var out []phpMatchRecord
	for i := range paramsNode.NamedChildCount() {
		param := paramsNode.NamedChild(i)
		if param == nil || param.Kind() != "property_promotion_parameter" {
			continue
		}

		// Collect visibility + readonly modifiers in source order.
		var modParts []string
		var visibility string
		for j := range param.ChildCount() {
			child := param.Child(j)
			if child == nil {
				continue
			}
			k := child.Kind()
			if !modifierKinds[k] {
				continue
			}
			text := child.Utf8Text(source)
			modParts = append(modParts, text)
			if k == "visibility_modifier" {
				visibility = text
			}
		}
		// If no visibility modifier, not a promoted property (guard).
		if visibility == "" {
			continue
		}

		nameNode := param.ChildByFieldName("name")
		if nameNode == nil {
			continue
		}

		// Build signature: <mods> [type] $name [= default] — identical shape to
		// phpPropertySignature so no caller can distinguish the two paths.
		var sb strings.Builder
		sb.WriteString(strings.Join(modParts, " "))

		typeNode := param.ChildByFieldName("type")
		if typeNode != nil {
			sb.WriteByte(' ')
			sb.WriteString(typeNode.Utf8Text(source))
		}

		nameText := nameNode.Utf8Text(source) // includes $
		sb.WriteByte(' ')
		sb.WriteString(nameText)

		defaultNode := param.ChildByFieldName("default_value")
		if defaultNode != nil {
			sb.WriteString(" = ")
			sb.WriteString(strings.TrimSpace(defaultNode.Utf8Text(source)))
		}

		// Symbol.Name strips the leading $ (same convention as regular properties).
		symbolName := strings.TrimPrefix(nameText, "$")

		out = append(out, phpMatchRecord{
			kind:      "property",
			name:      symbolName,
			signature: sb.String(),
			receiver:  receiver,
			exported:  phpVisibilityToExported(visibility),
			line:      int(param.StartPosition().Row) + 1,
			endLine:   int(param.EndPosition().Row) + 1,
			startByte: param.StartByte(),
		})
	}
	return out
}

// phpEnclosingClass walks up the AST from node to find the enclosing class,
// interface, trait, or enum name. Returns "" if not inside any type body.
func phpEnclosingClass(node *tree_sitter.Node, source []byte) string {
	enclosingKinds := map[string]bool{
		"class_declaration":     true,
		"interface_declaration": true,
		"trait_declaration":     true,
		"enum_declaration":      true,
	}
	cur := node.Parent()
	for cur != nil {
		if enclosingKinds[cur.Kind()] {
			if nameNode := cur.ChildByFieldName("name"); nameNode != nil {
				return nameNode.Utf8Text(source)
			}
			return ""
		}
		cur = cur.Parent()
	}
	return ""
}
