// provenance.go — THE at-rest provenance classification helper (TP §7.8, a
// W1 deliverable).
//
// Every consumer that renders a per-event time is obliged to classify the
// stored OccurredAt/TimeSource pair THROUGH THIS ONE FUNCTION, never inline
// per surface (TP §4.1 rule 2: the classification is made in ONE place).
// This is at-rest classification of stored bytes — NOT a renderer, and NOT
// the provenance × clock_state display-matrix helper (TP §7.4), which is
// W2's and CONSUMES this result. How an untrusted result is worded on a
// page is TP's header debt 2, owed at the first consumer build.
//
// Fail CLOSED at read (TP §4.1 rule 2): storage keeps received bytes exactly
// as sent — no CHECK constraint, no write-side normalisation — so the read
// side is where honesty happens. An unknown time_source (including any
// post-bump third enum value an older binary has never heard of) and the
// ill pair state (a "default" claim whose rule-W equality does not hold)
// both classify UntrustedProvenance, never Default: "observation" would be
// a positive certification the stored bytes cannot support.
package domain

// Provenance is the at-rest classification of one stored event's
// OccurredAt/TimeSource pair. The zero value is UntrustedProvenance —
// fail closed by construction.
type Provenance int

const (
	// UntrustedProvenance: the stored bytes do not support either honest
	// reading — an unknown time_source, or a "default" claim whose
	// OccurredAt != AuthoredAt equality is broken. Renders as UNTRUSTED
	// PROVENANCE, never as an observation (TP §7.4).
	UntrustedProvenance Provenance = iota
	// ProvenanceDefault: the officer left the time untouched and rule W's
	// equality holds — the machine stamped it. Rendered (with clock
	// context) as an observation.
	ProvenanceDefault
	// ProvenanceEntered: the officer explicitly supplied the time — an
	// operator's claim.
	ProvenanceEntered
)

func (p Provenance) String() string {
	switch p {
	case ProvenanceDefault:
		return "default"
	case ProvenanceEntered:
		return "entered"
	default:
		return "untrusted-provenance"
	}
}

// ProvenanceClass classifies one stored event's time provenance from the
// three stored values that define it. timeSource is the stored wire literal
// ("default"/"entered" are the only two any binary of this version writes);
// occurredAtMS and authoredAtMS are the stored OccurredAt and
// AuthoredAt.WallMS.
//
//   - "default" with occurredAtMS == authoredAtMS → ProvenanceDefault.
//   - "default" with the equality broken → UntrustedProvenance (the ill
//     pair state: rule W says the two are one copy of one reading, so a
//     mismatch means the record cannot be read as an observation).
//   - "entered" → ProvenanceEntered (the pair equality carries no meaning
//     here: an entered value may coincide with the stamp or not).
//   - anything else → UntrustedProvenance (fail closed; this is what lets
//     a third enum value be added post-bump without a schema outage).
func ProvenanceClass(timeSource string, occurredAtMS, authoredAtMS int64) Provenance {
	switch timeSource {
	case "default":
		if occurredAtMS == authoredAtMS {
			return ProvenanceDefault
		}
		return UntrustedProvenance
	case "entered":
		return ProvenanceEntered
	default:
		return UntrustedProvenance
	}
}
