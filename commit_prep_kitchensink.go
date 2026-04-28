package repomap

// commit_prep_kitchensink.go — Kitchen-sink guard: detects accidental fusion
// of unrelated files into a single commit group regardless of edge confidence.

import "strings"

// IsKitchenSink returns true when a CommitGroup looks like an accidental fusion
// that should be forced to LLM judgment. Triggers:
//  1. Group has more than 10 files.
//  2. Group spans more than one distinct top-level plugin segment
//     (e.g. plugins/dc/... and plugins/pi/... in the same group).
//  3. Group contains a plugin.json path — signals a new plugin being added.
func IsKitchenSink(g *CommitGroup) bool {
	if len(g.Files) > 10 {
		return true
	}
	if distinctPluginSegments(g.Files) > 1 {
		return true
	}
	for _, f := range g.Files {
		if strings.HasSuffix(f, "/plugin.json") || f == "plugin.json" {
			return true
		}
	}
	return false
}

// distinctPluginSegments counts how many unique plugin segments appear in paths
// (e.g. "dc" and "pi" each count as one). Returns 0 for files outside plugins/.
func distinctPluginSegments(files []string) int {
	seen := make(map[string]struct{}, 4)
	for _, f := range files {
		if seg := pluginSegment(f); seg != "" {
			seen[seg] = struct{}{}
		}
	}
	return len(seen)
}

// ContainsLowConf reports whether lowConf already contains an entry for groupID.
// Prevents double-adding a group that was already flagged by the confidence pass.
func ContainsLowConf(lowConf []PrepLowConf, groupID string) bool {
	for _, lc := range lowConf {
		if lc.GroupID == groupID {
			return true
		}
	}
	return false
}
