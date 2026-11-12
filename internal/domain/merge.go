// merge.go — the merge-resolution READ API, and the shared cycle-collapse
// that turns a set of authored `same_as` edges into a resolution snapshot.
//
// ⚠⚠ THIS FILE IS WHERE `AliasMap`, `Resolve` AND `maxAliasHops` NOW LIVE,
// AND THE MOVE IS THE POINT OF IT. W8 deletes the shared reference-merge
// mechanism — its event type, projection, table, fold, authoring functions
// and dev route — but P-3 §5 rules that only THAT mechanism dies: "the
// AliasMap shape and hop-capped Resolve survive as the read API over a new
// loader". Together those two sentences mean a MOVE, and no document
// assigned it, so the move is COMPILE-FORCED: W3's cycle guard binds against
// `maxAliasHops` (person_amend.go's `const maxMergeWalk = maxAliasHops`) and
// company_amend.go's bounded walk rides the same constant, so deleting them
// does not fail a test — IT FAILS TO COMPILE, IN TWO LOCKED CHUNKS' FILES.
//
// The destination is stated as a PROPERTY rather than a filename (W8-10(a)):
// the declaring file's base name does not contain `alias` — a file named for
// the deleted mechanism is the thing being corrected, not the location.
//
// ⚠ THE THREE SURVIVING SPELLINGS ARE ALSO THE ONES W8-9's census EXCLUDES BY
// NAME, and the exclusion is asserted with a negative arm. A census that
// flags `AliasMap` is broken; a build that deletes it does not compile. That
// is what stops the deletion criteria being satisfied by OVER-DELETING.
//
// ⚠ NOTHING HERE READS A TABLE. Every loader that feeds these shapes
// (LoadPersonMergeMap, LoadCompanyMergeMap, LoadVehicleMergeMap,
// LoadTradeMergeMap) derives its map PER READ from its own amendment side
// table's composed state, and caches nothing.
package domain

import "sitecontrol/internal/store"

// AliasMap is a duplicate-id -> canonical-id snapshot (one hop each) of a
// merge relation at a point in time. Load it ONCE per read (once per muster
// render, once per dump-compare run) and call Resolve repeatedly against it —
// not once per resolved id.
//
// The name is W3's and is deliberately UNCHANGED. It is the read API's shape,
// not the deleted mechanism's: renaming it would touch two locked chunks'
// files to no operator-visible end, and W8-9(d)'s negative arm asserts it
// survives the census by this exact spelling.
type AliasMap map[string]string

// maxAliasHops caps chain-following defensively. Chains are short by design
// and cycles are refused at every authoring edge, so this cap should never
// bind in practice; it exists so a corrupted or wire-formed chain can never
// make Resolve loop forever.
const maxAliasHops = 64

// Resolve follows the merge chain from id to its canonical record. An id with
// no outgoing edge resolves to itself (the common case: not a duplicate).
func (m AliasMap) Resolve(id string) string {
	cur := id
	for i := 0; i < maxAliasHops; i++ {
		next, ok := m[cur]
		if !ok {
			return cur
		}
		cur = next
	}
	return cur // hop cap hit: defensive stop, not expected to occur (see above)
}

// mergeEdge is one composed `same_as` assertion or un-assertion, carrying the
// log key that ordered it. Cleared edges are un-merges: Clear:["same_as"] IS
// the un-merge, and there is no separate event for it (P-3 §5).
type mergeEdge struct {
	Source  string
	Target  string
	Key     store.Key
	Cleared bool
}

// mergeMapFromEdges is the shared half of the derived resolution snapshot:
// effective-edge selection in record order, then wire-formed cycle collapse
// to ONE pinned canonical representative — the target of the cycle-forming
// assertion LATEST IN RECORD ORDER over the composed cycle set (W3-17's rule,
// as cloned at W5d-12).
//
// ⚠ IT IS SHARED BY THE TRADE LOADER ONLY, AND THAT IS A BOUND RATHER THAN AN
// OVERSIGHT. The person, company and vehicle loaders have their own landed
// copies of this walk inside LOCKED chunks' files; folding them into this one
// would be a refactor of code three certified ledgers rest on, at no
// operator-visible gain, and it is not W8's to make. What W8 owes is not to
// add a FOURTH copy, and it does not.
//
// ⟦SchemaVersion 11, [[D-92]]⟧ The trade loader now hands over ONE effective
// set-edge per source, chosen by resolveContest (causal first, then record
// order) — so the record-order selection below is vacuous for it and the
// cycle collapse is the whole of what this function does. The selection is
// kept as written: it is still correct for a caller that hands over the raw
// edge list, and removing it would change a LOCKED chunk's function for no
// gain.
//
// The rule the cycle branch implements: a cycle's members all resolve to the
// representative, and the representative resolves to itself.
func mergeMapFromEdges(edges []mergeEdge) AliasMap {
	type effective struct {
		target string
		key    store.Key
	}
	// Effective same_as per source: last assertion in record order wins; a
	// Clear removes the edge. Edges arrive already in ascending key order.
	eff := map[string]effective{}
	for _, e := range edges {
		if e.Cleared {
			delete(eff, e.Source)
			continue
		}
		eff[e.Source] = effective{target: e.Target, key: e.Key}
	}
	m := make(AliasMap, len(eff))
	edgeKey := make(map[string]store.Key, len(eff))
	for src, e := range eff {
		if e.target == src {
			continue // a wire-formed self-loop resolves nothing; drop the edge
		}
		m[src] = e.target
		edgeKey[src] = e.key
	}

	done := map[string]bool{}
	nodes := make([]string, 0, len(m))
	for n := range m {
		nodes = append(nodes, n)
	}
	for _, start := range nodes {
		if done[start] {
			continue
		}
		pathIdx := map[string]int{}
		var path []string
		cur := start
		for {
			if done[cur] {
				break
			}
			if i, in := pathIdx[cur]; in {
				// path[i:] is the cycle. The representative: the target of
				// the record-order-latest cycle edge.
				cycle := path[i:]
				var rep string
				var latest store.Key
				first := true
				for _, member := range cycle {
					k := edgeKey[member]
					if first || latest.Less(k) {
						rep = m[member]
						latest = k
						first = false
					}
				}
				for _, member := range cycle {
					if member == rep {
						delete(m, member) // the representative resolves to itself
					} else {
						m[member] = rep
					}
				}
				break
			}
			pathIdx[cur] = len(path)
			path = append(path, cur)
			next, ok := m[cur]
			if !ok {
				break
			}
			cur = next
		}
		for _, n := range path {
			done[n] = true
		}
	}
	return m
}
