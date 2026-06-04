package repomap

import (
	"context"
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
}

// AuditReadGroup is a compact first-read queue grouped by the kind of risk a
// local static packet found.
type AuditReadGroup struct {
	Group   string   `json:"group"`
	Lane    string   `json:"lane"`
	Reasons []string `json:"reasons"`
	Files   []string `json:"files"`
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
	return AuditBriefReport{
		SchemaVersion:  1,
		Root:           risks.Root,
		Risks:          compactBriefRisks(risks),
		Surface:        compactBriefSurface(surface),
		Effects:        compactBriefEffects(effects),
		FirstReadQueue: queue,
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
		if len(group.Files) > 12 {
			group.Files = group.Files[:12]
		}
		slices.Sort(group.Reasons)
		if len(group.Reasons) > 4 {
			group.Reasons = group.Reasons[:4]
		}
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
		if len(report.Lanes[i].Files) > 12 {
			report.Lanes[i].Files = report.Lanes[i].Files[:12]
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
		if len(report.Kinds[i].Files) > 12 {
			report.Kinds[i].Files = report.Kinds[i].Files[:12]
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
