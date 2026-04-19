; Classes — extended to capture base clause and interface clause
(class_declaration
  name: (name) @class.name
  (base_clause)? @class.base
  (class_interface_clause)? @class.interfaces) @class

; Interfaces — extended to capture base clause
(interface_declaration
  name: (name) @interface.name) @interface

; Traits
(trait_declaration
  name: (name) @trait.name) @trait

; Enums — backing type and interface clause are unnamed children, walked in Go
(enum_declaration
  name: (name) @enum.name) @enum

; Enum cases — backed or unbacked
(enum_case
  name: (name) @case.name) @case

; Free functions (not methods)
(function_definition
  name: (name) @function.name
  parameters: (formal_parameters) @function.params
  return_type: (_)? @function.return) @function

; Methods — modifiers are children (not fields), captured via Go node-walk
(method_declaration
  name: (name) @method.name
  parameters: (formal_parameters) @method.params
  return_type: (_)? @method.return) @method

; Properties — name lives inside property_element
(property_declaration
  type: (_)? @property.type
  (property_element name: (variable_name) @property.name)) @property

; Constants — name lives inside const_element
(const_declaration
  (const_element (name) @const.name)) @const

; Namespace declaration — populates FileSymbols.ImportPath for symbol-dep edges
(namespace_definition
  name: (namespace_name) @namespace)

; Use declarations — populate FileSymbols.Imports with fully-qualified names
; so symbol-dep edges can fire across files. Captures the imported FQCN; any
; alias (the `as` clause) is dropped because only the target identity matters.
(namespace_use_declaration
  (namespace_use_clause (qualified_name) @use))
(namespace_use_declaration
  (namespace_use_clause (name) @use))

; PHPDoc pairing — adjacency-based, no post-hoc walk
((comment) @doc
  . [(class_declaration) (interface_declaration) (trait_declaration)
     (enum_declaration) (enum_case) (function_definition) (method_declaration)
     (property_declaration) (const_declaration)] @doc.target
  (#match? @doc "^/\\*\\*"))
