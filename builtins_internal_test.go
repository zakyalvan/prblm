package prblm

import "testing"

// The sentinel table is ordered by non-descending status, so the lowest status
// wins within one error whose Is method reports several sentinels
// (error-sentinels / Requirement: Order when an error matches several
// built-ins). Each sentinel appears once.
func TestSentinelTable(t *testing.T) {
	t.Parallel()

	seen := make(map[error]bool, len(sentinels))

	for i, s := range sentinels {
		if i > 0 && s.status < sentinels[i-1].status {
			t.Errorf("sentinels[%d] (%v) has status %d, want at least %d", i, s.err, s.status, sentinels[i-1].status)
		}

		if seen[s.err] {
			t.Errorf("sentinels[%d] (%v) is listed twice", i, s.err)
		}

		seen[s.err] = true
	}
}
