package repomap

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// AuditBriefReport is the single-pass audit prepass packet used by workflow
// tools that need deterministic local context without rebuilding the map for
// every audit subcommand.
type AuditBriefReport struct {
	SchemaVersion  int                `json:"schema_version"`
	Root           string             `json:"root"`
	Risks          AuditRiskReport    `json:"risks"`
	Surface        AuditSurfaceReport `json:"surface"`
	Effects        AuditEffectReport  `json:"effects"`
	FirstReadQueue []AuditReadGroup   `json:"first_read_queue"`
	ReviewPlan     []AuditReviewLane  `json:"review_plan"`
}

// AuditReadGroup is a compact first-read queue grouped by the kind of risk a
// local static packet found.
type AuditReadGroup struct {
	ID            string   `json:"id"`
	Group         string   `json:"group"`
	Lane          string   `json:"lane"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Reasons       []string `json:"reasons"`
	Caveat        string   `json:"caveat,omitempty"`
	Files         []string `json:"files"`
	OmittedReason string   `json:"omitted_reason,omitempty"`
}

// AuditReviewLane is a deterministic per-lane review obligation derived from the
// first-read queue: which files to cover, what gates to discharge, and how to
// verify. It carries no findings — only obligations implied by the static packets.
type AuditReviewLane struct {
	ID            string   `json:"id"`
	Lane          string   `json:"lane"`
	Group         string   `json:"group"`
	EvidenceClass string   `json:"evidence_class,omitempty"`
	Confidence    string   `json:"confidence,omitempty"`
	Files         []string `json:"files"`
	Caveat        string   `json:"caveat,omitempty"`
	Gates         []string `json:"gates"`
	Verify        []string `json:"verify"`
	Why           []string `json:"why"`
	OmittedReason string   `json:"omitted_reason,omitempty"`
}

// AuditBrief computes risks, surface, effects, and a grouped first-read queue
// from one built Map.
func (m *Map) AuditBrief(ctx context.Context, limit int) (AuditBriefReport, error) {
	risks := m.AuditRisks(limit)
	surface, err := m.AuditSurface(ctx, limit)
	if err != nil {
		return AuditBriefReport{}, err
	}
	effects, err := m.AuditEffects(ctx, limit)
	if err != nil {
		return AuditBriefReport{}, err
	}
	queue := BuildAuditReadQueue(risks, surface, effects)
	goDetected := false
	for _, file := range risks.Files {
		if file.Language == "go" {
			goDetected = true
			break
		}
	}
	return AuditBriefReport{
		SchemaVersion:  2,
		Root:           risks.Root,
		Risks:          compactBriefRisks(risks),
		Surface:        compactBriefSurface(surface),
		Effects:        compactBriefEffects(effects),
		FirstReadQueue: queue,
		ReviewPlan:     BuildAuditReviewPlan(queue, goDetected),
	}, nil
}

// BuildAuditReadQueue turns audit packets into a deterministic file-read order
// grouped by why the files matter.
func BuildAuditReadQueue(risks AuditRiskReport, surface AuditSurfaceReport, effects AuditEffectReport) []AuditReadGroup {
	groups := map[string]*AuditReadGroup{}
	add := func(group, lane, reason string, files []string) {
		clean := make([]string, 0, len(files))
		for _, file := range files {
			if file == "" || isTestPath(file) {
				continue
			}
			clean = append(clean, file)
		}
		if len(clean) == 0 {
			return
		}
		entry := groups[group]
		if entry == nil {
			entry = &AuditReadGroup{Group: group, Lane: lane}
			groups[group] = entry
		}
		entry.Reasons = appendUnique(entry.Reasons, reason)
		entry.Files = append(entry.Files, clean...)
	}

	riskFiles := make([]string, 0, len(risks.Files))
	for _, file := range risks.Files {
		riskFiles = append(riskFiles, file.Path)
	}
	add("ranked-risk-packets", "architecture", "repomap ranked audit score", riskFiles)
	for _, lane := range risks.Lanes {
		switch lane.Name {
		case "test-risk":
			add("test-risk", "test-risk", lane.Reason, lane.Files)
		case "dead-code":
			add("dead-export-surface", "dead-code", lane.Reason, lane.Files)
		case "coupling":
			add("coupling-hotspots", "coupling", lane.Reason, lane.Files)
		case "parse-fidelity":
			add("parse-fidelity", "parse-fidelity", lane.Reason, lane.Files)
		}
	}

	addSurfaceGroup := func(group, lane, reason string, hits []AuditSurfaceHit) {
		files := make([]string, 0, len(hits))
		for _, hit := range hits {
			files = append(files, hit.Path)
		}
		add(group, lane, reason, files)
	}
	addSurfaceGroup("user-surface", "cli-ux", "commands, flags, and output paths", surface.Commands)
	addSurfaceGroup("user-surface", "cli-ux", "commands, flags, and output paths", surface.Flags)
	addSurfaceGroup("user-surface", "cli-ux", "commands, flags, and output paths", surface.Outputs)
	addSurfaceGroup("config-surface", "config", "env vars and config keys", surface.EnvVars)
	addSurfaceGroup("config-surface", "config", "env vars and config keys", surface.ConfigKeys)
	addSurfaceGroup("api-schema", "api-contracts", "routes and JSON schema fields", surface.Routes)
	addSurfaceGroup("api-schema", "api-contracts", "routes and JSON schema fields", surface.SchemaFields)
	addSurfaceGroup("dependency-policy", "dependency-policy", "dependency manifests and policy surface", surface.DependencyManifests)

	for _, kind := range effects.Kinds {
		switch kind.Name {
		case "filesystem-write", "database":
			add("writes-and-persistence", "data-integrity", kind.Reason, kind.Files)
		case "http", "serialization":
			add("network-and-api-effects", "api-contracts", kind.Reason, kind.Files)
		case "subprocess", "process-exit":
			add("subprocess-and-exit", "error-handling", kind.Reason, kind.Files)
		case "secret", "crypto", "randomness":
			add("secret-and-crypto", "security", kind.Reason, kind.Files)
		case "time", "filesystem-read":
			add("state-and-time", "data-integrity", kind.Reason, kind.Files)
		case "context-background", "goroutine":
			add("lifecycle-concurrency", "lifecycle-concurrency", kind.Reason, kind.Files)
		case "unbounded-read":
			add("resource-bounds", "performance", kind.Reason, kind.Files)
		}
	}

	priority := map[string]int{
		"user-surface":            0,
		"writes-and-persistence":  1,
		"network-and-api-effects": 2,
		"subprocess-and-exit":     3,
		"dependency-policy":       4,
		"config-surface":          5,
		"secret-and-crypto":       6,
		"lifecycle-concurrency":   7,
		"resource-bounds":         8,
		"test-risk":               9,
		"coupling-hotspots":       10,
		"dead-export-surface":     11,
		"parse-fidelity":          12,
		"ranked-risk-packets":     13,
		"state-and-time":          14,
		"api-schema":              15,
	}

	out := make([]AuditReadGroup, 0, len(groups))
	for _, group := range groups {
		group.Files = dedupeStrings(group.Files)
		if total := len(group.Files); total > 12 {
			group.Files = group.Files[:12]
			group.OmittedReason = fmt.Sprintf("showing 12 of %d files; truncated by brief cap", total)
		}
		slices.Sort(group.Reasons)
		if len(group.Reasons) > 4 {
			group.Reasons = group.Reasons[:4]
		}
		group.ID = "repomap:queue:" + auditSlug(group.Group)
		group.EvidenceClass = auditEvidenceForLanes([]string{group.Lane})
		group.Caveat = auditExternalCaveat([]string{group.Lane})
		group.Confidence = auditConfidence(group.EvidenceClass, group.Caveat != "")
		out = append(out, *group)
	}
	slices.SortFunc(out, func(a, b AuditReadGroup) int {
		ap, bp := priority[a.Group], priority[b.Group]
		if ap != bp {
			return ap - bp
		}
		return strings.Compare(a.Group, b.Group)
	})
	return out
}

// auditLanePlan is the static gates/verify obligation attached to a review lane.
// verify holds Go-specific commands emitted only when the target has Go sources.
type auditLanePlan struct {
	gates  []string
	verify []string
}

// auditReviewLaneTable maps each first-read-queue lane to its deterministic
// review obligations. Lanes absent here emit no gates/verify (never panic).
var auditReviewLaneTable = map[string]auditLanePlan{
	"lifecycle-concurrency": {gates: []string{"context propagation", "goroutine ownership", "shutdown cleanup", "channel-close ownership"}, verify: []string{"go test -race ./..."}},
	"cli-ux":                {gates: []string{"flag parsing", "help-text accuracy", "exit codes", "output formatting"}, verify: []string{"go build ./..."}},
	"api-contracts":         {gates: []string{"JSON schema stability", "encoder/decoder round-trip", "backward compatibility"}, verify: []string{"go test ./..."}},
	"data-integrity":        {gates: []string{"write atomicity", "read-after-write", "rollback on error"}, verify: []string{"go test ./..."}},
	"error-handling":        {gates: []string{"subprocess timeout", "stderr capture", "exit-code propagation", "error wrapping"}, verify: []string{"go vet ./...", "go test ./..."}},
	"security":              {gates: []string{"secret handling", "crypto correctness", "randomness source"}},
	"dependency-policy":     {gates: []string{"dependency necessity", "version pinning", "banned imports"}, verify: []string{"go list -m all"}},
	"performance":           {gates: []string{"bounded reads", "resource limits"}, verify: []string{"go test ./..."}},
	"config":                {gates: []string{"config defaults", "env-var precedence"}},
	"test-risk":             {gates: []string{"untested-export coverage"}, verify: []string{"go test ./..."}},
	"coupling":              {gates: []string{"fan-out justification", "interface boundaries"}},
	"dead-code":             {gates: []string{"confirm truly unused before removal"}},
	"parse-fidelity":        {gates: []string{"low-fidelity parse may miss symbols"}},
	"architecture":          {gates: []string{"central-dependency blast radius"}, verify: []string{"go build ./..."}},
}

// auditReviewLanePriority gives review lanes a deterministic, audit-meaningful order.
var auditReviewLanePriority = map[string]int{
	"cli-ux":                0,
	"api-contracts":         1,
	"data-integrity":        2,
	"error-handling":        3,
	"dependency-policy":     4,
	"config":                5,
	"security":              6,
	"lifecycle-concurrency": 7,
	"performance":           8,
	"test-risk":             9,
	"coupling":              10,
	"dead-code":             11,
	"parse-fidelity":        12,
	"architecture":          13,
}

// BuildAuditReviewPlan projects the first-read queue into per-lane review
// obligations: it merges read groups sharing a lane, attaches deterministic
// gates/verify from the static table, and suppresses Go-specific verify commands
// when the target has no Go sources. It invents no findings.
func BuildAuditReviewPlan(queue []AuditReadGroup, goDetected bool) []AuditReviewLane {
	type laneAcc struct {
		files []string
		why   []string
	}
	lanes := map[string]*laneAcc{}
	for _, group := range queue {
		acc := lanes[group.Lane]
		if acc == nil {
			acc = &laneAcc{}
			lanes[group.Lane] = acc
		}
		acc.files = append(acc.files, group.Files...)
		for _, reason := range group.Reasons {
			acc.why = appendUnique(acc.why, reason)
		}
	}

	out := make([]AuditReviewLane, 0, len(lanes))
	for lane, acc := range lanes {
		files := dedupeStrings(acc.files)
		omitted := ""
		if total := len(files); total > 12 {
			files = files[:12]
			omitted = fmt.Sprintf("showing 12 of %d files; truncated by brief cap", total)
		}
		why := acc.why
		slices.Sort(why)
		if len(why) > 4 {
			why = why[:4]
		}
		if why == nil {
			why = []string{}
		}
		plan := auditReviewLaneTable[lane]
		gates := plan.gates
		if gates == nil {
			gates = []string{}
		}
		verify := []string{}
		if goDetected && plan.verify != nil {
			verify = plan.verify
		}
		caveat := auditExternalCaveat([]string{lane})
		evidence := auditEvidenceForLanes([]string{lane})
		out = append(out, AuditReviewLane{
			ID:            "repomap:review:" + auditSlug(lane),
			Lane:          lane,
			Group:         lane,
			EvidenceClass: evidence,
			Confidence:    auditConfidence(evidence, caveat != ""),
			Files:         files,
			Caveat:        caveat,
			Gates:         gates,
			Verify:        verify,
			Why:           why,
			OmittedReason: omitted,
		})
	}
	slices.SortFunc(out, func(a, b AuditReviewLane) int {
		ap, bp := auditReviewLanePriority[a.Lane], auditReviewLanePriority[b.Lane]
		if ap != bp {
			return ap - bp
		}
		return strings.Compare(a.Lane, b.Lane)
	})
	return out
}

func dedupeStrings(items []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

func compactBriefRisks(report AuditRiskReport) AuditRiskReport {
	for i := range report.Lanes {
		if total := len(report.Lanes[i].Files); total > 12 {
			report.Lanes[i].Files = report.Lanes[i].Files[:12]
			report.Lanes[i].OmittedReason = fmt.Sprintf("showing 12 of %d files; truncated by brief cap", total)
		}
	}
	return report
}

func compactBriefSurface(report AuditSurfaceReport) AuditSurfaceReport {
	report.Commands = capBriefSurface(report.Commands, 24)
	report.Flags = capBriefSurface(report.Flags, 32)
	report.EnvVars = capBriefSurface(report.EnvVars, 32)
	report.ConfigKeys = capBriefSurface(report.ConfigKeys, 40)
	report.SchemaFields = capBriefSurface(report.SchemaFields, 40)
	report.Routes = capBriefSurface(report.Routes, 32)
	report.Outputs = capBriefSurface(report.Outputs, 32)
	report.DependencyManifests = capBriefSurface(report.DependencyManifests, 16)
	return report
}

func compactBriefEffects(report AuditEffectReport) AuditEffectReport {
	for i := range report.Kinds {
		if total := len(report.Kinds[i].Files); total > 12 {
			report.Kinds[i].Files = report.Kinds[i].Files[:12]
			report.Kinds[i].OmittedReason = fmt.Sprintf("showing 12 of %d files; truncated by brief cap", total)
		}
	}
	return report
}

func capBriefSurface(items []AuditSurfaceHit, limit int) []AuditSurfaceHit {
	if len(items) > limit {
		return items[:limit]
	}
	return items
}
