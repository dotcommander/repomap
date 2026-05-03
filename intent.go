package repomap

import (
	"math"
	"path/filepath"
	"slices"
	"strings"
)

const (
	bm25K1 = 1.2
	bm25B  = 0.75
)

// intentDoc is the keyword index for a single file.
type intentDoc struct {
	terms   map[string]float64 // term → weighted term frequency
	totalTF float64            // sum of all weighted TFs (document length proxy)
}

// IntentScorer holds the corpus index and scores files against a query.
type IntentScorer struct {
	docs  []intentDoc    // parallel to ranked slice
	avgDL float64        // average document length
	n     int            // total documents
	df    map[string]int // document frequency per term
}

// NewIntentScorer builds the per-file keyword index from ranked files.
func NewIntentScorer(ranked []RankedFile) *IntentScorer {
	docs := make([]intentDoc, len(ranked))
	df := make(map[string]int)

	for i, rf := range ranked {
		terms := make(map[string]float64)

		// Field: basename (weight 3)
		base := strings.ToLower(strings.TrimSuffix(filepath.Base(rf.Path), filepath.Ext(rf.Path)))
		for _, tok := range tokenizeCamelCase(base) {
			terms[tok] += 3.0
		}

		// Field: package name (weight 3)
		if rf.Package != "" {
			for _, tok := range tokenizeCamelCase(rf.Package) {
				terms[tok] += 3.0
			}
		}

		// Field: exported symbol names (weight 2)
		for _, sym := range rf.Symbols {
			if sym.Exported {
				for _, tok := range tokenizeCamelCase(sym.Name) {
					terms[tok] += 2.0
				}
			}
		}

		// Field: import paths last segment (weight 1)
		for _, imp := range rf.Imports {
			seg := imp
			if idx := strings.LastIndex(imp, "/"); idx >= 0 {
				seg = imp[idx+1:]
			}
			for _, tok := range tokenizeIntent(seg) {
				terms[tok] += 1.0
			}
		}

		// Field: struct field names from Signature (weight 1)
		for _, sym := range rf.Symbols {
			if sym.Signature != "" {
				for _, tok := range tokenizeSignatureFields(sym.Signature) {
					terms[tok] += 1.0
				}
			}
		}

		var totalTF float64
		for _, v := range terms {
			totalTF += v
		}
		docs[i] = intentDoc{terms: terms, totalTF: totalTF}

		// Update document frequency
		for term := range terms {
			df[term]++
		}
	}

	var sumDL float64
	for _, d := range docs {
		sumDL += d.totalTF
	}
	avgDL := 1.0
	if len(docs) > 0 {
		avgDL = sumDL / float64(len(docs))
	}

	return &IntentScorer{
		docs:  docs,
		avgDL: avgDL,
		n:     len(ranked),
		df:    df,
	}
}

// Score re-ranks files in place by multiplying base scores with BM25 relevance.
// Returns the same slice (mutated) sorted by final_score descending.
func (s *IntentScorer) Score(ranked []RankedFile, query string) []RankedFile {
	if query == "" || len(ranked) == 0 {
		return ranked
	}

	rawTokens := tokenizeIntent(query)
	if len(rawTokens) == 0 {
		return ranked
	}
	queryTokens, negated := extractNegated(rawTokens)
	if len(queryTokens) == 0 {
		return ranked
	}

	// Build negation set for fast lookup
	negSet := make(map[string]bool, len(negated))
	for _, t := range negated {
		negSet[t] = true
	}

	bm25Scores := make([]float64, len(ranked))
	N := float64(s.n)

	for i, doc := range s.docs {
		var score float64
		for _, token := range queryTokens {
			tf := weightedTermFrequency(doc, token)
			if tf == 0 {
				continue
			}
			dfCount := float64(s.df[token])
			idf := math.Log((N-dfCount+0.5)/(dfCount+0.5) + 1)
			dl := doc.totalTF
			score += (tf * (bm25K1 + 1)) / (tf + bm25K1*(1-bm25B+bm25B*dl/s.avgDL)) * idf
		}

		// Bigram bonus
		for j := 0; j+1 < len(queryTokens); j++ {
			t1, t2 := queryTokens[j], queryTokens[j+1]
			if bigramExists(doc, t1, t2) {
				df1 := float64(s.df[t1])
				df2 := float64(s.df[t2])
				idf1 := math.Log((N-df1+0.5)/(df1+0.5) + 1)
				idf2 := math.Log((N-df2+0.5)/(df2+0.5) + 1)
				avgIDF := (idf1 + idf2) / 2
				bonus := avgIDF
				if bonus < 1.5 {
					bonus = 1.5
				}
				score += bonus
			}
		}

		bm25Scores[i] = score
	}

	// Normalize to 0..1
	maxScore := 0.0
	for _, s := range bm25Scores {
		if s > maxScore {
			maxScore = s
		}
	}

	for i := range ranked {
		var boost float64
		if maxScore > 0 {
			boost = bm25Scores[i] / maxScore
		}

		// Penalize negated files: if file matches any negated term, reduce boost
		doc := s.docs[i]
		for negTerm := range negSet {
			if weightedTermFrequency(doc, negTerm) > 0 {
				boost *= 0.1
				break
			}
		}

		ranked[i].Score = int(float64(ranked[i].Score) * (1.0 + boost))
	}

	// Sort by Score descending, Path ascending for ties
	slices.SortStableFunc(ranked, func(a, b RankedFile) int {
		if b.Score != a.Score {
			if b.Score > a.Score {
				return 1
			}
			return -1
		}
		return strings.Compare(a.Path, b.Path)
	})

	return ranked
}

// weightedTermFrequency returns the weighted TF for a query token against a doc,
// applying fuzzy matching (plural, singular, prefix/stem).
func weightedTermFrequency(doc intentDoc, queryToken string) float64 {
	// Exact match
	if tf, ok := doc.terms[queryToken]; ok {
		return tf
	}

	// Plural match
	plural := queryToken + "s"
	if tf, ok := doc.terms[plural]; ok {
		return tf * 0.9
	}

	// Singular match (strip trailing s)
	if strings.HasSuffix(queryToken, "s") {
		singular := strings.TrimSuffix(queryToken, "s")
		if singular != queryToken {
			if tf, ok := doc.terms[singular]; ok {
				return tf * 0.9
			}
		}
	}

	// Prefix/stem match (min 4 chars)
	if len(queryToken) >= 4 {
		prefix4 := queryToken[:4]
		bestTF := 0.0
		for term, tf := range doc.terms {
			termLen := len(term)
			if termLen < 4 {
				continue
			}
			if strings.HasPrefix(term, prefix4) || strings.HasPrefix(queryToken, term[:min(4, termLen)]) {
				if tf*0.6 > bestTF {
					bestTF = tf * 0.6
				}
			}
		}
		return bestTF
	}

	return 0.0
}

// bigramExists returns true if both terms appear in the document (adjacent in
// at least one field is not tracked; this is a co-occurrence proxy).
func bigramExists(doc intentDoc, t1, t2 string) bool {
	return weightedTermFrequency(doc, t1) > 0 && weightedTermFrequency(doc, t2) > 0
}
