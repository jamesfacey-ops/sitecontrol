// w5c_created_half_test.go — W5c-4, THE CREATED HALF, in the domain —
// CORRECTED TO THE SCHEMA-11 SHAPE.
//
//	W5c-4 ⟦was⟧ PersonCreated carries PadOriented nullable as *string (DR-8);
//	       the `person` projection gains the nullable TEXT column.
//	W5c-4 ⟦SchemaVersion 11⟧ PersonCreated carries NO PadOriented field and
//	       never marshals the `pad_oriented` key; the `person` projection has
//	       NO such column; a wire person_created still carrying the stale key
//	       FOLDS (never refused) and the key lands NOWHERE.
//
// ⟦SchemaVersion 11 — CORRECTED, NOT DELETED, 2026-09-05⟧ [[D-88]]'s ERRATA
// struck the fact: one flag on the ENROLLING site's person row, named after
// one site, is structurally the wrong record for a per-site orientation
// ("once per person, per site, forever, taken AT that site"), and under the
// D-87 choke point only the enroller could ever have recorded a Pad
// orientation. D-88 reverses D-10 and D-21.1, the rulings this file's old
// assertions rested on. Every assertion below is the old one INVERTED onto
// the new shape — presence became absence, "recorded as it arrives" became
// "lands nowhere, and the fold still never refuses" — so the file keeps its
// four teeth and its precondition guard rather than losing them.
//
// The SCHEMA half (introspection over the whole table) is asserted in
// internal/store/w5c_projection_and_version_test.go. This file carries the
// two halves that only the domain can see: THE PAYLOAD SHAPE and THE FOLD.
//
// MUTANTS THIS FILE KILLS: (m5c-4′) the field or column re-added under the
// same or a renamed spelling · (m5c-6) a fold that REFUSES a wire
// person_created still carrying the stale key (W3-2 forked).
//
// ⚠ NO SELECTOR ON THE STRUCK FIELD APPEARS BELOW — it cannot, the field is
// gone; every assertion is reflection over the struct types, raw table
// introspection, or a wire envelope through store.Apply. Machinery and seams:
// w5c_seams_test.go.
package domain

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"sitecontrol/internal/store"
)

// w5cPersonCreatedField finds a field on PersonCreated BY NAME through
// reflection — never as a selector.
func w5cPersonCreatedField(name string) (reflect.StructField, bool) {
	return reflect.TypeOf(PersonCreated{}).FieldByName(name)
}

// TestW5cPersonCreatedCarriesNoPadOrientedField — W5c-4, the PAYLOAD-SHAPE
// half, inverted: the struct declares NO field named PadOriented and NO
// field tagged `pad_oriented`, under any Go type.
// ⟦was: TestW5cPersonCreatedCarriesPadOrientedAsNullableString⟧
//
// The positive control is the surviving neighbour `Phone`: the same probe
// must FIND it, or "not found" is the probe being blind rather than the
// field being gone.
func TestW5cPersonCreatedCarriesNoPadOrientedField(t *testing.T) {
	if _, ok := w5cPersonCreatedField("Phone"); !ok {
		t.Fatal("CONTROL FAILED: the reflection probe cannot see PersonCreated.Phone, a field that exists — it could not then vouch for an absence")
	}
	if f, ok := w5cPersonCreatedField(w5cGoFieldPadOriented); ok {
		t.Fatalf("W5c-4 (m5c-4′): PersonCreated still declares %s (%s, json %q). The fact is STRUCK at SchemaVersion 11 ([[D-88]] ERRATA / [[D-91]]): a flag on the enrolling site's person row cannot be a per-site orientation, and the ruled shape is the ORIENTING post's own record, built in a later stage of this increment.", w5cGoFieldPadOriented, f.Type, f.Tag.Get("json"))
	}
	rt := reflect.TypeOf(PersonCreated{})
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if strings.HasPrefix(f.Tag.Get("json"), w5cKeyPadOriented) || strings.Contains(f.Name, "Oriented") {
			t.Errorf("W5c-4 (m5c-4′): PersonCreated.%s (json %q) re-introduces the struck fact under another spelling", f.Name, f.Tag.Get("json"))
		}
	}
}

// TestW5cPersonCreatedNeverMarshalsThePadOrientedKey — W5c-4's byte half,
// inverted: a FULLY POPULATED person_created payload marshals no
// `pad_oriented` key. Populating every optional field is the mirror that
// keeps this honest — a struct that marshals nothing at all also carries no
// such key.
// ⟦was: TestW5cPersonCreatedOmitsPadOrientedWhenAbsent⟧
func TestW5cPersonCreatedNeverMarshalsThePadOrientedKey(t *testing.T) {
	v := reflect.New(reflect.TypeOf(PersonCreated{})).Elem()
	v.FieldByName("Type").SetString(w5cEvtPersonCreated)
	v.FieldByName("PersonID").SetString("site-a-person-full")
	v.FieldByName("Name").SetString("Aurora, Ada")
	populated := 0
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() == reflect.Pointer && f.Type().Elem().Kind() == reflect.String && f.IsNil() {
			val := "x"
			f.Set(reflect.ValueOf(&val))
			populated++
		}
	}
	if populated < 5 {
		t.Fatalf("fixture defect: populated only %d optional *string fields — the payload must be FULL for the absence below to mean anything", populated)
	}
	keys := w5cJSONKeys(t, v.Interface())
	if !keys["phone"] || !keys["last_name"] {
		t.Fatalf("CONTROL FAILED: the fully populated payload does not carry `phone`/`last_name` — the marshal is not exercising the optional fields, so its silence on %q proves nothing (keys: %v)", w5cKeyPadOriented, keys)
	}
	if keys[w5cKeyPadOriented] {
		t.Fatalf("W5c-4 (m5c-4′): a person_created payload marshals the struck key %q — the wire must not carry a fact the record no longer holds (gate B-2's frozen key set loses this key at SchemaVersion 11)", w5cKeyPadOriented)
	}
}

// TestW5cCreatedHalfFoldWritesNoPadOrientedColumn — W5c-4, the FOLD half,
// inverted. A wire person_created that still carries the stale key FOLDS
// (the fold never refuses, W3-2) and the raw `person` row has no column for
// it: the key lands NOWHERE, and the record is byte-identical to one whose
// person_created never carried the key. The mirror keeps the fold honest:
// `phone`, a surviving column, still lands from the same envelope.
// ⟦was: TestW5cCreatedHalfFoldWritesPadOriented⟧
func TestW5cCreatedHalfFoldWritesNoPadOrientedColumn(t *testing.T) {
	st := w5cNewStore(t)
	w5cRequireStruckShape(t, st, "W5c-4 (fold half)")

	stale := w5cSeedPerson(t, st, 1, "Aurora, Ada", map[string]any{w5cKeyPadOriented: w5cOriented, "phone": "555 0100"})
	plain := w5cSeedPerson(t, st, 2, "Aurora, Ada", map[string]any{"phone": "555 0100"})

	if cols := w5cTableColumns(t, st, "person"); w5cHasColumn(cols, w5cKeyPadOriented) {
		t.Fatalf("W5c-4 (m5c-4′): the `person` projection still carries %q: %v", w5cKeyPadOriented, cols)
	}
	staleRow, plainRow := w5cRawPersonRow(t, st, stale), w5cRawPersonRow(t, st, plain)
	if staleRow.Phone == nil || *staleRow.Phone != "555 0100" {
		t.Fatalf("CONTROL FAILED (mirror): the fold did not write `phone` from an envelope that also carried the stale key — the fold is not landing the envelope at all, so \"the stale key landed nowhere\" would be true for the wrong reason (read back: %s)", w5cPtrStr(staleRow.Phone))
	}
	staleRow.PersonID, plainRow.PersonID = "", ""
	sk, pk := w5cJSONKeys(t, staleRow), w5cJSONKeys(t, plainRow)
	if fmt.Sprint(sk) != fmt.Sprint(pk) {
		t.Fatalf("W5c-4: the raw rows of a person whose person_created carried the stale key and one whose did not differ in KEY SET — the stale key found somewhere to land.\n  stale: %v\n  plain: %v", sk, pk)
	}
	if sk[w5cKeyPadOriented] {
		t.Fatalf("W5c-4 (m5c-4′): the raw `person` row carries %q", w5cKeyPadOriented)
	}
	if ck := w5cJSONKeys(t, w5cComposed(t, st, stale)); ck[w5cKeyPadOriented] {
		t.Fatalf("W5c-4: the COMPOSED person carries %q — composition resurrected a fact the record does not hold", w5cKeyPadOriented)
	}
}

// TestW5cWirePersonCreatedWithTheStaleKeyStillLands — W3-2 on the created
// half, unchanged in principle and inverted in subject: whatever value the
// stale key carries, the fold lands the person (never refuses — a refusal
// forks the two logs permanently) and records the value nowhere. The LOG
// keeps the bytes as sent, which is the only place a stale key may survive.
// ⟦was: TestW5cWirePersonCreatedRecordsWhateverArrives⟧
//
// EXPECTED RED: a builder who "cleans up" the strike by refusing envelopes
// that still carry the old key (m5c-6) — the fold must stay tolerant.
func TestW5cWirePersonCreatedWithTheStaleKeyStillLands(t *testing.T) {
	for i, arriving := range []string{"true", "false", "0", "no", "TRUE"} {
		arriving := arriving
		t.Run(fmt.Sprintf("wire_%02d_%s", i, w5cLabel(arriving)), func(t *testing.T) {
			st := w5cNewStore(t)
			w5cRequireStruckShape(t, st, "W3-2 (created-half stale-key arm)")
			id := w5cSeedPerson(t, st, 1, "Aurora, Ada", map[string]any{w5cKeyPadOriented: arriving})
			row := w5cRawPersonRow(t, st, id) // fatals if the fold did not land the person (m5c-6)
			if row.Name != "Aurora, Ada" {
				t.Fatalf("the landed row is not the person sent: %+v", row)
			}
			if keys := w5cJSONKeys(t, row); keys[w5cKeyPadOriented] {
				t.Fatalf("W5c-4: a peer's stale %s=%q reached the raw `person` row — the strike left a landing place", w5cKeyPadOriented, arriving)
			}
			// The log — and only the log — still holds the bytes as sent.
			authored := w5cAuthoredPadValues(t, st, "site-b", 7)
			if len(authored) != 1 {
				t.Fatalf("fixture defect: the wire envelope carrying the stale key is not in the log as sent (found %v) — received bytes are stored exactly as received", authored)
			}
		})
	}
}

var _ = func(st *store.Store) {}
