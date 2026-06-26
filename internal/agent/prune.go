package agent

import (
	"fmt"
	"strings"

	"reasonix/internal/provider"
)

// Pruning is the free half of context maintenance: stale tool results are
// re-derivable (files can be re-read, commands re-run), so eliding them needs
// no summarizer call and never drops a message — tool_call/result pairing and
// assistant content (including signed reasoning) are untouched by construction.
const (
	prunedMarker  = "[elided tool result — "
	minPruneBytes = 1024
)

// PruneStats reports one prune pass.
type PruneStats struct {
	Results    int
	SavedChars int
	Archive    string
}

// PruneStaleToolResults elides tool-result content older than the protected
// recent tail, archiving the originals first. Idempotent; a no-op when
// compaction is disabled (no context window).
//
// 这样做可以在不改动 cache-stable system prefix 的前提下回收上下文，但代价是
// 较老的 inline skill body 不是永久保留对象：一旦它们离开受保护的 recent
// tail，就可能像其他大 tool result 一样被归档后替换成占位标记。这里追求的
// 约束是“可恢复、可续跑”（archive / summary / re-run），而不是“逐字永久保留”。
func (a *Agent) PruneStaleToolResults() (PruneStats, error) {
	var st PruneStats
	if a.contextWindow <= 0 {
		return st, nil
	}
	msgs := a.session.Messages
	head, start, ok := a.planCompaction(msgs, 1)
	if !ok {
		return st, nil
	}
	var idx []int
	for i := head; i < start; i++ {
		m := msgs[i]
		if m.Role != provider.RoleTool || len(m.Content) < minPruneBytes || strings.HasPrefix(m.Content, prunedMarker) {
			continue
		}
		// Honor the keep policy before pruning: an error:/blocked: tool result
		// that KeepErrors would preserve must reach compact() verbatim.
		// Eliding it here rewrites Content to the [elided ...] marker, so the
		// KeepErrors predicate sees only the placeholder and the failure is
		// lost on the next fold. Long build/test failures sit beyond the recent
		// tail, exactly this range.
		if a.keepPolicy&KeepErrors != 0 && isErrorMessage(m) {
			continue
		}
		idx = append(idx, i)
	}
	if len(idx) == 0 {
		return st, nil
	}
	if a.archiveDir != "" {
		originals := make([]provider.Message, 0, len(idx))
		for _, i := range idx {
			originals = append(originals, msgs[i])
		}
		path, err := archiveMessages(a.archiveDir, originals)
		if err != nil {
			return st, fmt.Errorf("archive: %w", err)
		}
		st.Archive = path
	}
	next := append([]provider.Message(nil), msgs...)
	for _, i := range idx {
		m := next[i]
		placeholder := fmt.Sprintf("%s%s, %d bytes dropped to save context; re-run the tool if the data is needed again]", prunedMarker, m.Name, len(m.Content))
		st.SavedChars += len(m.Content) - len(placeholder)
		m.Content = placeholder
		next[i] = m
		st.Results++
	}
	a.session.Replace(next)
	a.session.IncrementRewrite()
	return st, nil
}
