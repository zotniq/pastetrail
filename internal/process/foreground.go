package process

// foregroundApp resolves a job only when its leader is the common ancestor of
// every foreground member. Sibling pipeline commands remain ambiguous.
func foregroundApp(foreground, all []entry, shellPID int) (entry, bool) {
	if len(foreground) == 1 {
		return foreground[0], true
	}
	if len(foreground) == 0 {
		return entry{}, false
	}
	byPID := make(map[int]entry, len(all))
	for _, p := range all {
		byPID[p.pid] = p
	}
	var leader entry
	for _, p := range foreground {
		if p.pid == p.group {
			leader = p
			break
		}
	}
	if leader.pid == 0 || leader.pid == shellPID {
		return entry{}, false
	}
	for _, p := range foreground {
		seen := make(map[int]bool)
		for p.pid != leader.pid {
			if seen[p.pid] {
				return entry{}, false
			}
			seen[p.pid] = true
			var ok bool
			p, ok = byPID[p.parent]
			if !ok {
				return entry{}, false
			}
		}
	}
	return leader, true
}
