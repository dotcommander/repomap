package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCFamilyParsers(t *testing.T) {
	t.Parallel()

	t.Run("parseRust", func(t *testing.T) {
		lines := []string{
			"// Rust file",
			"use std::collections::HashMap;",
			"pub struct Config {",
			"    pub host: String,",
			"}",
			"pub async fn start_server() {",
			"}",
			"pub enum Status { Active, Inactive }",
			"impl Config {",
			"    pub fn new() -> Self { Config { host: String::new() } }",
			"}",
		}
		fs := &FileSymbols{Language: "rust"}
		parseRust(lines, fs)

		assert.Contains(t, fs.Imports, "std::collections::HashMap")
		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "start_server")
		assert.Contains(t, symNames, "Config")
		assert.Contains(t, symNames, "Status")
	})

	t.Run("parseC", func(t *testing.T) {
		lines := []string{
			"#include <stdio.h>",
			"#include \"config.h\"",
			"struct Vector { int x; int y; };",
			"enum Mode { FAST, SLOW };",
			"int compute_val(int a, int b) {",
			"    return a + b;",
			"}",
		}
		fs := &FileSymbols{Language: "c"}
		parseC(lines, fs)

		assert.Contains(t, fs.Imports, "stdio.h")
		assert.Contains(t, fs.Imports, "config.h")

		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "Vector")
		assert.Contains(t, symNames, "Mode")
		assert.Contains(t, symNames, "compute_val")
	})

	t.Run("parseJava", func(t *testing.T) {
		lines := []string{
			"package com.example;",
			"import java.util.List;",
			"import static java.lang.Math.max;",
			"public class Manager {",
			"    public static void processItems() {",
			"    }",
			"}",
			"public interface Executable {",
			"}",
			"public record Point(int x, int y) {}",
		}
		fs := &FileSymbols{Language: "java"}
		parseJava(lines, fs)

		assert.Contains(t, fs.Imports, "java.util.List")
		assert.Contains(t, fs.Imports, "java.lang.Math.max")

		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "Manager")
		assert.Contains(t, symNames, "processItems")
		assert.Contains(t, symNames, "Executable")
		assert.Contains(t, symNames, "Point")
	})
}

func TestWebParsers(t *testing.T) {
	t.Parallel()

	t.Run("parsePHP", func(t *testing.T) {
		lines := []string{
			"<?php",
			"namespace App\\Services;",
			"use App\\Contracts\\Handler;",
			"class WorkerService {",
			"    public function __construct() {}",
			"    public function executeTask() {}",
			"}",
			"const MAX_RETRY = 5;",
			"interface WorkItem {}",
			"trait Loggable {}",
			"enum State {}",
		}
		fs := &FileSymbols{Language: "php"}
		parsePHP(lines, fs)

		assert.Equal(t, "App\\Services", fs.Package)
		assert.Contains(t, fs.Imports, "App\\Contracts\\Handler")

		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "WorkerService")
		assert.Contains(t, symNames, "executeTask")
		assert.NotContains(t, symNames, "__construct")
		assert.Contains(t, symNames, "MAX_RETRY")
		assert.Contains(t, symNames, "WorkItem")
		assert.Contains(t, symNames, "Loggable")
		assert.Contains(t, symNames, "State")
	})

	t.Run("parseRuby", func(t *testing.T) {
		lines := []string{
			"# Ruby script",
			"module Auth",
			"  class Authenticator",
			"    def authenticate_user",
			"    end",
			"  end",
			"end",
		}
		fs := &FileSymbols{Language: "ruby"}
		parseRuby(lines, fs)

		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "Auth")
		assert.Contains(t, symNames, "Authenticator")
		assert.Contains(t, symNames, "authenticate_user")
	})
}

func TestTSAndGenericParsers(t *testing.T) {
	t.Parallel()

	t.Run("parseTS", func(t *testing.T) {
		lines := []string{
			"import { useState } from 'react';",
			"const path = require('path');",
			"export function renderApp() {}",
			"export class AppContainer {}",
			"export default class MainApp {}",
			"export { HelperFunc, UtilityClass as Util };",
		}
		fs := &FileSymbols{Language: "typescript"}
		parseTS(lines, fs)

		assert.Contains(t, fs.Imports, "react")
		assert.Contains(t, fs.Imports, "path")

		symNames := make([]string, len(fs.Symbols))
		for i, s := range fs.Symbols {
			symNames[i] = s.Name
		}
		assert.Contains(t, symNames, "renderApp")
		assert.Contains(t, symNames, "AppContainer")
		assert.Contains(t, symNames, "MainApp")
		assert.Contains(t, symNames, "HelperFunc")
		assert.Contains(t, symNames, "UtilityClass")
	})

	t.Run("splitReExportNames", func(t *testing.T) {
		names := splitReExportNames("foo, bar as baz, qux")
		assert.Equal(t, []string{"foo", "bar", "qux"}, names)
	})
}

func TestCtagsHelperFunctions(t *testing.T) {
	t.Parallel()

	t.Run("mapCtagsKind", func(t *testing.T) {
		assert.Equal(t, "function", mapCtagsKind("function"))
		assert.Equal(t, "function", mapCtagsKind("f"))
		assert.Equal(t, "method", mapCtagsKind("m"))
		assert.Equal(t, "class", mapCtagsKind("c"))
		assert.Equal(t, "struct", mapCtagsKind("s"))
		assert.Equal(t, "interface", mapCtagsKind("i"))
		assert.Equal(t, "enum", mapCtagsKind("enum"))
		assert.Equal(t, "enum", mapCtagsKind("g"))
		assert.Equal(t, "variable", mapCtagsKind("v"))
		assert.Equal(t, "constant", mapCtagsKind("d"))
		assert.Equal(t, "type", mapCtagsKind("typedef"))
		assert.Equal(t, "", mapCtagsKind("unknown_kind"))
	})

	t.Run("isMemberKind", func(t *testing.T) {
		assert.True(t, isMemberKind("member"))
		assert.True(t, isMemberKind("field"))
		assert.False(t, isMemberKind("function"))
	})

	t.Run("isScopeContainer", func(t *testing.T) {
		assert.True(t, isScopeContainer("struct"))
		assert.True(t, isScopeContainer("class"))
		assert.False(t, isScopeContainer("function"))
	})

	t.Run("isExportedForLang", func(t *testing.T) {
		assert.False(t, isExportedForLang("", "go"))
		assert.True(t, isExportedForLang("ExportedName", "go"))
		assert.False(t, isExportedForLang("unexported", "go"))
		assert.False(t, isExportedForLang("_private", "python"))
		assert.True(t, isExportedForLang("public_fn", "python"))
		assert.True(t, isExportedForLang("anything", "javascript"))
	})
}
