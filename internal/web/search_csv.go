// search_csv.go — GET /search.csv: one result group of the CURRENT search as
// a CSV download (FIX16 stream B; the writer's rules are csvdownload.go's).
//
// SAME ROWS AS THE PAGE. The handler calls buildSearch — the function
// /search renders from — with the same query string, so the search, the
// "Narrow results" filters and the "show all" expansion all apply exactly as
// they do on screen: a group the page cuts at 50 is cut at 50 here too, and
// its "Show all" link's page offers the full download. The columns are the
// page table's columns, cell for cell — a post by its display name, as the
// page's sitelabel prints it (Cfg.SiteName); the only difference is that a cell
// the page fills with a note for an absent value ("none on file", "(no
// company recorded)") is left blank.
//
// group= names the table: people, visits or trips. Every other parameter is
// the page's own.
package web

import (
	"net/http"
	"net/url"
	"strings"
)

// searchCSVHrefs is each group's download link; "" when no results.
type searchCSVHrefs struct{ People, Visits, Trips string }

// searchCSVLinks builds the three links from the page's own query string.
func searchCSVLinks(q url.Values) searchCSVHrefs {
	link := func(group string) string {
		v := url.Values{}
		for k, vals := range q {
			if k != "group" {
				v[k] = vals
			}
		}
		v.Set("group", group)
		return "/search.csv?" + v.Encode()
	}
	return searchCSVHrefs{People: link("people"), Visits: link("visits"), Trips: link("trips")}
}

var (
	searchPeopleCSVHeader = []string{"Name", "Company", "Trade", "Phone", "Where", "Enrolled at"}
	searchVisitsCSVHeader = []string{"Person", "Post", "Time in", "Time out", "Truck", "Company (at check-in)"}
	searchTripsCSVHeader  = []string{"Truck", "From → to", "Dispatched", "Arrived", "Driver", "Aboard", "Status"}
)

// withHand is a time cell with its "entered by hand" marker, as the page
// prints it.
func withHand(text, hand string) string {
	if hand == "" {
		return text
	}
	return text + " " + hand
}

func searchPeopleCSV(rows []searchPersonRow, siteName func(string) string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		name := r.Name
		if r.Retired {
			name += " (retired)"
		}
		out = append(out, []string{name, r.Company, r.Trade, r.Phone, r.Where, siteName(r.EnrolledAt)})
	}
	return out
}

func searchVisitsCSV(rows []searchVisitRow, siteName func(string) string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, []string{r.Person, siteName(r.Site), withHand(r.In, r.InHand),
			withHand(r.Out, r.OutHand), r.Vehicle, r.Company})
	}
	return out
}

func searchTripsCSV(rows []searchTripRow, siteName func(string) string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, r := range rows {
		arrived := withHand(r.Arrived, r.ArrHand)
		if r.Arrived == "" {
			arrived = "not arrived"
		}
		aboard := strings.Join(r.Aboard, ", ")
		if aboard == "" {
			aboard = "driver only"
		}
		out = append(out, []string{r.Vehicle, siteName(r.Origin) + " → " + r.Destination,
			withHand(r.Dispatched, r.DispHand), arrived, r.Driver, aboard, r.Status})
	}
	return out
}

func (s *Server) handleSearchCSV(w http.ResponseWriter, r *http.Request) {
	data := s.buildSearch(r)
	switch {
	case !data.Searched:
		http.Error(w, "nothing was searched — run a search on /search first, then download its results", http.StatusBadRequest)
		return
	case len(data.Problems) > 0:
		http.Error(w, "not searched — "+strings.Join(data.Problems, " "), http.StatusBadRequest)
		return
	case data.Err != "":
		// Loud, never an empty file that reads as "nobody matched".
		http.Error(w, "search failed — "+data.Err+". No file was produced.", http.StatusInternalServerError)
		return
	}
	group := r.URL.Query().Get("group")
	switch {
	case group == "people" && data.Narrow.ShowPeople:
		s.writeCSVDownload(w, "search-people", searchPeopleCSVHeader, searchPeopleCSV(data.People, s.Cfg.SiteName))
	case group == "visits" && data.Narrow.ShowVisits:
		s.writeCSVDownload(w, "search-visits", searchVisitsCSVHeader, searchVisitsCSV(data.Visits, s.Cfg.SiteName))
	case group == "trips" && data.Narrow.ShowTrips:
		s.writeCSVDownload(w, "search-trips", searchTripsCSVHeader, searchTripsCSV(data.Trips, s.Cfg.SiteName))
	default:
		http.Error(w, "group must be people, visits or trips, and shown by the current filters", http.StatusBadRequest)
	}
}
