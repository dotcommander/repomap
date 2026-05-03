package repomap

import (
	"strings"
	"unicode"
)

// intentStopwords are terms that appear in almost every Go file and add no
// discriminating signal to BM25 scoring.
var intentStopwords = map[string]bool{
	// Go keywords
	"func": true, "return": true, "error": true, "string": true,
	"int": true, "bool": true, "nil": true, "var": true,
	"const": true, "type": true, "struct": true, "interface": true,
	"package": true, "import": true, "if": true, "else": true,
	"for": true, "range": true, "switch": true, "case": true,
	"defer": true, "go": true, "chan": true, "map": true,
	"select": true, "break": true, "continue": true, "fallthrough": true,
	// Common English stopwords
	"the": true, "a": true, "an": true, "is": true, "are": true,
	"was": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true,
	"did": true, "will": true, "would": true, "could": true, "should": true,
	"may": true, "might": true, "shall": true, "can": true,
	"this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true, "in": true, "on": true, "at": true,
	"to": true, "of": true, "with": true, "from": true, "by": true,
	"and": true, "or": true, "but": true, "so": true, "yet": true,
	"as": true, "into": true, "about": true,
	// Generic programming terms (noise in code search)
	"file": true, "files": true, "code": true, "implement": true,
	"add": true, "fix": true, "update": true, "change": true,
	"make": true, "use": true, "using": true, "get": true, "set": true,
}

// tokenizeIntent splits text into lowercase tokens, splitting on non-alphanumeric
// characters (preserving hyphens within words), then drops stopwords.
func tokenizeIntent(s string) []string {
	s = strings.ToLower(s)
	var tokens []string
	var cur strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' {
			cur.WriteRune(r)
		} else {
			if cur.Len() > 0 {
				tok := strings.Trim(cur.String(), "-")
				if tok != "" && !intentStopwords[tok] {
					tokens = append(tokens, tok)
				}
				cur.Reset()
			}
		}
	}
	if cur.Len() > 0 {
		tok := strings.Trim(cur.String(), "-")
		if tok != "" && !intentStopwords[tok] {
			tokens = append(tokens, tok)
		}
	}
	return tokens
}

// tokenizeCamelCase splits a CamelCase or snake_case identifier into component words.
// Examples:
//
//	"ParseGoFile" → ["parse", "go", "file"]
//	"http_client" → ["http", "client"]
//	"ALLCAPS"     → ["allcaps"]
func tokenizeCamelCase(s string) []string {
	// First split on underscores and hyphens
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-'
	})
	var tokens []string
	for _, part := range parts {
		tokens = append(tokens, splitCamel(part)...)
	}
	return tokens
}

// splitCamel splits a single CamelCase word into lowercase tokens.
func splitCamel(s string) []string {
	if s == "" {
		return nil
	}
	runes := []rune(s)
	var tokens []string
	start := 0
	for i := 1; i < len(runes); i++ {
		if unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i-1]) {
			tokens = append(tokens, strings.ToLower(string(runes[start:i])))
			start = i
		} else if i+1 < len(runes) && unicode.IsUpper(runes[i]) && unicode.IsLower(runes[i+1]) && unicode.IsUpper(runes[i-1]) {
			tokens = append(tokens, strings.ToLower(string(runes[start:i])))
			start = i
		}
	}
	tokens = append(tokens, strings.ToLower(string(runes[start:])))
	// Filter empty
	out := tokens[:0]
	for _, t := range tokens {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// tokenizeSignatureFields extracts parameter and field names from a Symbol.Signature string.
// Handles formats like "(ctx context.Context, name string) error" or "{Name string, ID int}".
func tokenizeSignatureFields(sig string) []string {
	// Strip outer parens/braces
	sig = strings.TrimSpace(sig)
	sig = strings.TrimPrefix(sig, "(")
	sig = strings.TrimPrefix(sig, "{")
	sig = strings.TrimSuffix(sig, ")")
	sig = strings.TrimSuffix(sig, "}")

	// Split on commas to get individual fields/params
	var tokens []string
	for _, field := range strings.Split(sig, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		// First token is the name (before the type)
		parts := strings.Fields(field)
		if len(parts) > 0 {
			for _, t := range tokenizeCamelCase(parts[0]) {
				if !intentStopwords[t] && len(t) > 1 {
					tokens = append(tokens, t)
				}
			}
		}
	}
	return tokens
}

// extractNegated splits tokens into those to keep and those that were
// preceded by a negation word ("not", "without", "except", "no").
func extractNegated(tokens []string) (keep, negated []string) {
	negationWords := map[string]bool{
		"not": true, "without": true, "except": true, "no": true,
	}
	negate := false
	for _, t := range tokens {
		if negationWords[t] {
			negate = true
			continue
		}
		if negate {
			negated = append(negated, t)
			negate = false
		} else {
			keep = append(keep, t)
		}
	}
	return keep, negated
}
