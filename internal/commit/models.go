// Package commit owns commit mutation, preparation, release, and verification
// workflows. Analysis models remain in the public repomap package.
package commit

import "github.com/dotcommander/repomap"

const (
	PrepStatusReady         = "ready"
	PrepStatusNeedsJudgment = "needs_judgment"
	PrepStatusAbort         = "abort"
)

type CommitAnalysis = repomap.CommitAnalysis
type CommitGroup = repomap.CommitGroup
type CommitRefs = repomap.CommitRefs
type Finding = repomap.Finding
type SecretsSummary = repomap.SecretsSummary

const (
	ActionFix     = repomap.ActionFix
	ActionSafe    = repomap.ActionSafe
	ActionReview  = repomap.ActionReview
	VerdictSafe   = repomap.VerdictSafe
	VerdictUnsafe = repomap.VerdictUnsafe
)
