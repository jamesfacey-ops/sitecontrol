package web

import "sitecontrol/internal/config"

// importRecordsOrientation is design Q3, kept in its own file so James's
// answer changes one function (lead, 2026-10-06: "keep each one isolated").
//
// At an orientation post the one-person intake form IS the orientation
// record (R-12: "no one will be placed in the system without the
// oriantation") — the officer enrols somebody standing at the desk who has
// just been oriented. A spreadsheet of contractors is not that: nobody on it
// has been oriented at this desk. The default answer is therefore that an
// import records NO orientation anywhere; the people it adds are held out at
// every orientation post until that post ticks them on /orientation, exactly
// like somebody enrolled at a road post.
func importRecordsOrientation(*config.Config) bool { return false }
