//go:build !notreesitter

package repomap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPHPSpike_DocAdjacency verifies that the adjacency-based PHPDoc pairing
// works end-to-end: a docblocked class is returned with Kind=class and a Doc
// extracted from the first sentence of the /** comment.
func TestPHPSpike_DocAdjacency(t *testing.T) {
	t.Parallel()

	src := []byte(`<?php

/**
 * Handles incoming HTTP requests for the user resource.
 * @param string $id
 */
class UserController {
    public function index() {}
}

function helperFunc() {}
`)

	fs := parsePHPWithTreeSitter(src, "src/UserController.php")
	require.NotNil(t, fs, "parsePHPWithTreeSitter must return a non-nil FileSymbols")
	require.NotEmpty(t, fs.Symbols, "expected at least one symbol")

	// Find the class symbol.
	var classSymbol *Symbol
	for i := range fs.Symbols {
		if fs.Symbols[i].Kind == "class" {
			classSymbol = &fs.Symbols[i]
			break
		}
	}
	require.NotNil(t, classSymbol, "expected a symbol with Kind=class")

	assert.Equal(t, "UserController", classSymbol.Name)
	assert.Equal(t, "class", classSymbol.Kind)
	assert.True(t, classSymbol.Exported, "PHP classes should be exported=true in spike")
	assert.NotEmpty(t, classSymbol.Doc, "class with PHPDoc should have Doc populated")
	assert.Contains(t, classSymbol.Doc, "Handles incoming HTTP requests")
}

// findSymbol returns the first symbol matching kind and name, or nil.
func findSymbol(symbols []Symbol, kind, name string) *Symbol {
	for i := range symbols {
		if symbols[i].Kind == kind && symbols[i].Name == name {
			return &symbols[i]
		}
	}
	return nil
}

// TestPHPCoreKinds exercises class/method/property/const signatures.
func TestPHPCoreKinds(t *testing.T) {
	t.Parallel()

	t.Run("class_with_extends_implements", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo extends BaseRepo implements Repo, Countable {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "class", "UserRepo")
		require.NotNil(t, sym, "expected class symbol UserRepo")
		assert.Equal(t, "class UserRepo extends BaseRepo implements Repo, Countable", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("abstract_class", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
abstract class BaseRepo {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "class", "BaseRepo")
		require.NotNil(t, sym, "expected class symbol BaseRepo")
		assert.Equal(t, "abstract class BaseRepo", sym.Signature)
	})

	t.Run("public_method", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo {
    public function save(int $id): ?User {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "save")
		require.NotNil(t, sym, "expected method symbol save")
		assert.Equal(t, "public function save(int $id): ?User", sym.Signature)
		assert.True(t, sym.Exported)
		assert.Equal(t, "UserRepo", sym.Receiver)
	})

	t.Run("static_method", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo {
    public static function find(int $id): ?User {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "find")
		require.NotNil(t, sym, "expected method symbol find")
		assert.Equal(t, "public static function find(int $id): ?User", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("abstract_method", func(t *testing.T) {
		t.Parallel()
		// Abstract methods have no body — verify query still matches.
		src := []byte(`<?php
abstract class BaseRepo {
    abstract protected function load(string $key): array;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "load")
		require.NotNil(t, sym, "expected method symbol load (abstract)")
		assert.Contains(t, sym.Signature, "function load")
		assert.Contains(t, sym.Signature, "string $key")
	})

	t.Run("private_method_not_exported", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo {
    private function secret(): void {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "secret")
		require.NotNil(t, sym, "expected method symbol secret")
		assert.False(t, sym.Exported, "private method must not be exported")
		assert.Contains(t, sym.Signature, "private function secret")
	})

	t.Run("protected_method_not_exported", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo {
    protected function internal(): void {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "internal")
		require.NotNil(t, sym, "expected method symbol internal")
		assert.False(t, sym.Exported, "protected method must not be exported")
	})

	t.Run("readonly_typed_property", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class User {
    public readonly string $name;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "property", "name")
		require.NotNil(t, sym, "expected property symbol name")
		assert.Equal(t, "public readonly string $name", sym.Signature)
		assert.True(t, sym.Exported)
		assert.Equal(t, "User", sym.Receiver)
	})

	t.Run("property_with_default", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class Config {
    public string $driver = 'mysql';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "property", "driver")
		require.NotNil(t, sym, "expected property symbol driver")
		assert.Contains(t, sym.Signature, "= ")
		assert.Contains(t, sym.Signature, "'mysql'")
	})

	t.Run("class_const", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class App {
    const VERSION = '1.0';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "const", "VERSION")
		require.NotNil(t, sym, "expected const symbol VERSION")
		assert.Equal(t, "const VERSION = '1.0'", sym.Signature)
		assert.True(t, sym.Exported)
		assert.Equal(t, "App", sym.Receiver)
	})

	t.Run("typed_const_php83", func(t *testing.T) {
		t.Parallel()
		// PHP 8.3 typed class constants.
		src := []byte(`<?php
class App {
    public const string VERSION = '2.0';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "const", "VERSION")
		require.NotNil(t, sym, "expected const symbol VERSION")
		// With type hint the signature should contain the type.
		assert.Contains(t, sym.Signature, "VERSION")
		assert.Contains(t, sym.Signature, "= '2.0'")
	})

	t.Run("method_receiver_is_class_name", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class OrderService {
    public function process(Order $order): void {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "process")
		require.NotNil(t, sym, "expected method symbol process")
		assert.Equal(t, "OrderService", sym.Receiver, "Receiver must equal the enclosing class name")
	})

	t.Run("public_const_visibility", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class App {
    public const API_URL = 'https://api.example.com';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "const", "API_URL")
		require.NotNil(t, sym, "expected const symbol API_URL")
		assert.Contains(t, sym.Signature, "public const")
		assert.True(t, sym.Exported)
	})
}

// TestPHPExtendedKinds covers interface, trait, enum, enum case, and full
// function signatures added in task 4.
func TestPHPExtendedKinds(t *testing.T) {
	t.Parallel()

	t.Run("function_with_signature", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
function find(int $id, ?string $type = null): ?User {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "function", "find")
		require.NotNil(t, sym, "expected function symbol find")
		assert.Equal(t, "function find(int $id, ?string $type = null): ?User", sym.Signature)
		assert.True(t, sym.Exported)
		assert.Equal(t, "", sym.Receiver)
	})

	t.Run("interface_with_extends", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
interface Repo extends BaseRepo, Countable {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "interface", "Repo")
		require.NotNil(t, sym, "expected interface symbol Repo")
		assert.Equal(t, "interface Repo extends BaseRepo, Countable", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("interface_method_abstract", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
interface Repo {
    public function save(User $user): void;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "save")
		require.NotNil(t, sym, "expected abstract method save inside interface")
		assert.Equal(t, "Repo", sym.Receiver, "method inside interface must have Receiver = interface name")
		assert.Contains(t, sym.Signature, "function save")
	})

	t.Run("trait_basic", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
trait Cacheable {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "trait", "Cacheable")
		require.NotNil(t, sym, "expected trait symbol Cacheable")
		assert.Equal(t, "trait Cacheable", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("trait_method_receiver", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
trait Cacheable {
    public function getCacheKey(): string {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "getCacheKey")
		require.NotNil(t, sym, "expected method getCacheKey inside trait")
		assert.Equal(t, "Cacheable", sym.Receiver, "method inside trait must have Receiver = trait name")
	})

	t.Run("enum_pure", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "enum", "Status")
		require.NotNil(t, sym, "expected enum symbol Status")
		assert.Equal(t, "enum Status", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("enum_backed_string", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "enum", "Status")
		require.NotNil(t, sym, "expected enum symbol Status")
		assert.Equal(t, "enum Status: string", sym.Signature)
	})

	t.Run("enum_backed_int", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Priority: int {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "enum", "Priority")
		require.NotNil(t, sym, "expected enum symbol Priority")
		assert.Equal(t, "enum Priority: int", sym.Signature)
	})

	t.Run("enum_implements", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string implements HasLabel {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "enum", "Status")
		require.NotNil(t, sym, "expected enum symbol Status")
		assert.Equal(t, "enum Status: string implements HasLabel", sym.Signature)
	})

	t.Run("enum_case_string_backed", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string {
    case Active = 'active';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "case", "Active")
		require.NotNil(t, sym, "expected case symbol Active")
		assert.Equal(t, "case Active = 'active'", sym.Signature)
		assert.True(t, sym.Exported)
	})

	t.Run("enum_case_int_backed", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Priority: int {
    case Low = 1;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "case", "Low")
		require.NotNil(t, sym, "expected case symbol Low")
		assert.Equal(t, "case Low = 1", sym.Signature)
	})

	t.Run("enum_case_unbacked", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Color {
    case Red;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "case", "Red")
		require.NotNil(t, sym, "expected case symbol Red")
		assert.Equal(t, "case Red", sym.Signature)
	})

	t.Run("enum_case_receiver_is_enum", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string {
    case Active = 'active';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "case", "Active")
		require.NotNil(t, sym, "expected case symbol Active")
		assert.Equal(t, "Status", sym.Receiver, "enum case Receiver must equal the enclosing enum name")
	})

	t.Run("enum_method_receiver_is_enum", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string {
    case Active = 'active';
    public function label(): string {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "label")
		require.NotNil(t, sym, "expected method label inside enum")
		assert.Equal(t, "Status", sym.Receiver, "method inside enum must have Receiver = enum name")
	})
}

// TestPHPConstructorPromotion verifies that PHP 8.0+ constructor property
// promotion emits both the constructor method AND one property symbol per
// promoted parameter, with signatures byte-identical to explicitly-declared
// regular properties of the same conceptual field.
func TestPHPConstructorPromotion(t *testing.T) {
	t.Parallel()

	t.Run("promoted_single_public_readonly_typed", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class User {
    public function __construct(
        public readonly string $name,
    ) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		// Constructor method must still exist.
		ctor := findSymbol(fs.Symbols, "method", "__construct")
		require.NotNil(t, ctor, "constructor method symbol must be present")

		// Exactly one promoted property.
		prop := findSymbol(fs.Symbols, "property", "name")
		require.NotNil(t, prop, "promoted property 'name' must be emitted")
		assert.Equal(t, "public readonly string $name", prop.Signature)
		assert.True(t, prop.Exported)
		assert.Equal(t, "User", prop.Receiver)
	})

	t.Run("promoted_multiple_mixed_visibility", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class Order {
    public function __construct(
        public string $id,
        protected int $count,
        private ?string $note,
    ) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		pub := findSymbol(fs.Symbols, "property", "id")
		require.NotNil(t, pub, "promoted public property 'id' must be emitted")
		assert.True(t, pub.Exported, "public promoted property must be Exported=true")

		prot := findSymbol(fs.Symbols, "property", "count")
		require.NotNil(t, prot, "promoted protected property 'count' must be emitted")
		assert.False(t, prot.Exported, "protected promoted property must be Exported=false")

		priv := findSymbol(fs.Symbols, "property", "note")
		require.NotNil(t, priv, "promoted private property 'note' must be emitted")
		assert.False(t, priv.Exported, "private promoted property must be Exported=false")
	})

	t.Run("promoted_with_defaults", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class Config {
    public function __construct(
        public int $id = 0,
    ) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		prop := findSymbol(fs.Symbols, "property", "id")
		require.NotNil(t, prop, "promoted property 'id' must be emitted")
		assert.Contains(t, prop.Signature, "= 0", "promoted property signature must include default value")
	})

	t.Run("promoted_mixed_with_plain_params", func(t *testing.T) {
		t.Parallel()
		// Plain params (no visibility modifier) must NOT become properties.
		src := []byte(`<?php
class Handler {
    public function __construct(
        public string $name,
        string $temporary,
        int $alsoPlain,
    ) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		prop := findSymbol(fs.Symbols, "property", "name")
		require.NotNil(t, prop, "promoted property 'name' must be emitted")

		// Plain params must NOT appear as properties.
		assert.Nil(t, findSymbol(fs.Symbols, "property", "temporary"), "plain param must not become a property")
		assert.Nil(t, findSymbol(fs.Symbols, "property", "alsoPlain"), "plain param must not become a property")
	})

	t.Run("promoted_receiver_is_class_name", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class PaymentGateway {
    public function __construct(
        public string $apiKey,
    ) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		prop := findSymbol(fs.Symbols, "property", "apiKey")
		require.NotNil(t, prop, "promoted property 'apiKey' must be emitted")
		assert.Equal(t, "PaymentGateway", prop.Receiver, "promoted property Receiver must equal enclosing class name")
	})

	t.Run("promoted_line_is_param_line", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class Foo {
    public function __construct(
        public string $bar,
    ) {}
}
`)
		// $bar is on line 4 (1-indexed).
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		prop := findSymbol(fs.Symbols, "property", "bar")
		require.NotNil(t, prop, "promoted property 'bar' must be emitted")
		assert.Equal(t, 4, prop.Line, "promoted property Line must equal the parameter's line, not the constructor's")
	})

	t.Run("no_promotion_no_extra_properties", func(t *testing.T) {
		t.Parallel()
		// Constructor with only plain params — no promoted properties.
		src := []byte(`<?php
class Plain {
    public string $explicit;

    public function __construct(string $x, int $y) {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)

		ctor := findSymbol(fs.Symbols, "method", "__construct")
		require.NotNil(t, ctor, "constructor method must be present")

		// Only one property: the explicitly declared one.
		var props []Symbol
		for _, s := range fs.Symbols {
			if s.Kind == "property" {
				props = append(props, s)
			}
		}
		require.Len(t, props, 1, "only the explicit property should exist, no extras from plain constructor params")
		assert.Equal(t, "explicit", props[0].Name)
	})

	t.Run("promoted_signature_matches_regular_property", func(t *testing.T) {
		t.Parallel()
		// A promoted and an explicit property with the same declaration shape
		// must have byte-identical signatures.
		srcPromoted := []byte(`<?php
class A {
    public function __construct(
        public readonly string $name,
    ) {}
}
`)
		srcExplicit := []byte(`<?php
class B {
    public readonly string $name;
}
`)
		fsP := parsePHPWithTreeSitter(srcPromoted, "a.php")
		fsE := parsePHPWithTreeSitter(srcExplicit, "b.php")
		require.NotNil(t, fsP)
		require.NotNil(t, fsE)

		promoted := findSymbol(fsP.Symbols, "property", "name")
		explicit := findSymbol(fsE.Symbols, "property", "name")
		require.NotNil(t, promoted, "promoted property must be found")
		require.NotNil(t, explicit, "explicit property must be found")

		assert.Equal(t, explicit.Signature, promoted.Signature,
			"promoted and explicit property signatures must be byte-identical")
	})
}

// TestPHPDoc verifies PHPDoc extraction: tag stripping, sentence truncation,
// multi-line joining, and adjacency pairing across all 9 supported kinds.
func TestPHPDoc(t *testing.T) {
	t.Parallel()

	t.Run("phpdoc_summary_with_tags", func(t *testing.T) {
		t.Parallel()
		// Full PHPDoc with @param/@return/@throws — tags must be stripped.
		src := []byte(`<?php
/**
 * Find a user by ID.
 *
 * @param int $id The user ID.
 * @param ?string $type Optional user type.
 * @return ?User The user, or null if not found.
 * @throws UserNotFoundException
 */
function findUser(int $id): ?User {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "function", "findUser")
		require.NotNil(t, sym, "expected function symbol findUser")
		assert.Equal(t, "Find a user by ID.", sym.Doc)
	})

	t.Run("phpdoc_single_line", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
/** Find a user. */
function findUser(): ?User {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "function", "findUser")
		require.NotNil(t, sym, "expected function symbol findUser")
		assert.Equal(t, "Find a user.", sym.Doc)
	})

	t.Run("phpdoc_multiline_wrap", func(t *testing.T) {
		t.Parallel()
		// Multi-line prose (no tags) — lines joined with space.
		src := []byte(`<?php
/**
 * Find a user by ID,
 * falling back if not found.
 */
function findUser(): ?User {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "function", "findUser")
		require.NotNil(t, sym, "expected function symbol findUser")
		assert.Equal(t, "Find a user by ID, falling back if not found.", sym.Doc)
	})

	t.Run("phpdoc_tags_only", func(t *testing.T) {
		t.Parallel()
		// No prose summary — only @tag lines. Doc must be empty.
		src := []byte(`<?php
/**
 * @param int $id
 * @return ?User
 */
function findUser(int $id): ?User {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "function", "findUser")
		require.NotNil(t, sym, "expected function symbol findUser")
		assert.Equal(t, "", sym.Doc, "tag-only PHPDoc must yield empty Doc")
	})

	t.Run("phpdoc_on_method", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class UserRepo {
    /**
     * Persist a user record.
     * @param User $user
     */
    public function save(User $user): void {}
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "method", "save")
		require.NotNil(t, sym, "expected method symbol save")
		assert.Equal(t, "Persist a user record.", sym.Doc)
	})

	t.Run("phpdoc_on_property", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class User {
    /** The user's display name. */
    public string $name;
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "property", "name")
		require.NotNil(t, sym, "expected property symbol name")
		assert.Equal(t, "The user's display name.", sym.Doc)
	})

	t.Run("phpdoc_on_const", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
class App {
    /** Current API version. */
    const VERSION = '1.0';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "const", "VERSION")
		require.NotNil(t, sym, "expected const symbol VERSION")
		assert.Equal(t, "Current API version.", sym.Doc)
	})

	t.Run("phpdoc_on_interface", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
/** Defines the repository contract. */
interface Repo {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "interface", "Repo")
		require.NotNil(t, sym, "expected interface symbol Repo")
		assert.Equal(t, "Defines the repository contract.", sym.Doc)
	})

	t.Run("phpdoc_on_trait", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
/** Adds caching behaviour to a class. */
trait Cacheable {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "trait", "Cacheable")
		require.NotNil(t, sym, "expected trait symbol Cacheable")
		assert.Equal(t, "Adds caching behaviour to a class.", sym.Doc)
	})

	t.Run("phpdoc_on_enum", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
/** Represents order status values. */
enum Status: string {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "enum", "Status")
		require.NotNil(t, sym, "expected enum symbol Status")
		assert.Equal(t, "Represents order status values.", sym.Doc)
	})

	t.Run("phpdoc_on_enum_case", func(t *testing.T) {
		t.Parallel()
		src := []byte(`<?php
enum Status: string {
    /** The active state. */
    case Active = 'active';
}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		sym := findSymbol(fs.Symbols, "case", "Active")
		require.NotNil(t, sym, "expected case symbol Active")
		assert.Equal(t, "The active state.", sym.Doc)
	})

	t.Run("non_phpdoc_comment_ignored", func(t *testing.T) {
		t.Parallel()
		// Neither // nor /* */ (non-PHPDoc) should populate Doc.
		src := []byte(`<?php
// Single-line comment above function.
function alpha(): void {}

/* Block comment, not a docblock. */
function beta(): void {}
`)
		fs := parsePHPWithTreeSitter(src, "test.php")
		require.NotNil(t, fs)
		alpha := findSymbol(fs.Symbols, "function", "alpha")
		require.NotNil(t, alpha, "expected function symbol alpha")
		assert.Equal(t, "", alpha.Doc, "// comment must not populate Doc")

		beta := findSymbol(fs.Symbols, "function", "beta")
		require.NotNil(t, beta, "expected function symbol beta")
		assert.Equal(t, "", beta.Doc, "/* */ comment must not populate Doc")
	})
}
