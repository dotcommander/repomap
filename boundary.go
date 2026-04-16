package repomap

import "strings"

// boundaryRule maps well-known import prefixes to a semantic boundary label
// and a per-file score bump awarded when any prefix in the rule matches.
type boundaryRule struct {
	Label     string   // e.g. "HTTP", "Postgres"
	Prefixes  []string // import path prefixes that trigger this label
	ScoreBump int      // score added when any prefix matches
}

// boundaryRules is the classification table for Go imports.
// Ordered for deterministic label output; callers must not assume order
// beyond what this slice defines.
var boundaryRules = []boundaryRule{
	{
		Label:     "HTTP",
		Prefixes:  []string{"net/http", "github.com/go-chi/chi", "github.com/gin-gonic/gin", "github.com/gorilla/mux"},
		ScoreBump: 5,
	},
	{
		Label:     "Postgres",
		Prefixes:  []string{"github.com/jackc/pgx", "database/sql", "github.com/lib/pq"},
		ScoreBump: 5,
	},
	{
		Label:     "Redis",
		Prefixes:  []string{"github.com/redis/", "github.com/go-redis/"},
		ScoreBump: 5,
	},
	{
		Label:     "Kafka",
		Prefixes:  []string{"github.com/segmentio/kafka-go", "github.com/IBM/sarama", "github.com/Shopify/sarama"},
		ScoreBump: 5,
	},
	{
		Label:     "gRPC",
		Prefixes:  []string{"google.golang.org/grpc"},
		ScoreBump: 5,
	},
	{
		Label:     "Shell",
		Prefixes:  []string{"os/exec"},
		ScoreBump: 3,
	},
	{
		Label:     "Crypto",
		Prefixes:  []string{"crypto/", "golang.org/x/crypto"},
		ScoreBump: 3,
	},
}

// maxBoundaryBump is the maximum total score bump a single file can earn
// from boundary classification, regardless of how many boundaries match.
const maxBoundaryBump = 15

// classifyBoundaries inspects imports and returns the set of boundary labels
// present (in boundaryRules order) and the total score bump (capped at
// maxBoundaryBump). Each label is emitted at most once even if multiple
// imports in the same file match the same rule.
func classifyBoundaries(imports []string) (labels []string, bump int) {
	for _, rule := range boundaryRules {
		matched := false
		for _, imp := range imports {
			for _, prefix := range rule.Prefixes {
				if strings.HasPrefix(imp, prefix) {
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
		if matched {
			labels = append(labels, rule.Label)
			bump += rule.ScoreBump
		}
	}
	if bump > maxBoundaryBump {
		bump = maxBoundaryBump
	}
	return labels, bump
}
