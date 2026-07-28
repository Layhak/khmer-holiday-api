// Command scrape fetches Cambodian public holidays and stores them.
//
// Subcommands:
//
//	scrape   fetch one or more years from the configured sources
//	status   show coverage and the per-source audit trail
//	verify   promote a year to "official" after checking the sub-decree
//	sources  list the configured sources
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/httpx"
	"github.com/layhak/khmer-holiday-api/internal/model"
	"github.com/layhak/khmer-holiday-api/internal/sources"
	"github.com/layhak/khmer-holiday-api/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var err error
	switch os.Args[1] {
	case "scrape":
		err = cmdScrape(ctx, os.Args[2:])
	case "status":
		err = cmdStatus(ctx, os.Args[2:])
	case "verify":
		err = cmdVerify(ctx, os.Args[2:])
	case "sources":
		err = cmdSources(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `khapi-scrape - fetch and manage Cambodian public holiday data

Usage:
  khapi-scrape scrape  [-year N] [-years A-B] [-source NAME] [-replace] [-db PATH]
  khapi-scrape status  [-db PATH]
  khapi-scrape verify  -year N -decree "Sub-Decree No. 167" [-url URL] [-db PATH]
  khapi-scrape sources

Examples:
  khapi-scrape scrape -year 2027
  khapi-scrape scrape -years 2024-2027
  khapi-scrape scrape -year 2027 -source mlvt
  khapi-scrape status
  khapi-scrape verify -year 2026 -decree "Sub-Decree No. 167" \
      -url https://mlvt.gov.kh/media/k2/attachments/20250918_216.pdf
`)
}

// dbFlag registers the shared -db flag.
func dbFlag(fs *flag.FlagSet) *string {
	def := os.Getenv("KHAPI_DB")
	if def == "" {
		def = "data/holidays.db"
	}
	return fs.String("db", def, "path to the SQLite database")
}

func cmdScrape(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("scrape", flag.ExitOnError)
	var (
		year    = fs.Int("year", 0, "year to fetch (default: current and next year)")
		yearRng = fs.String("years", "", "inclusive year range, e.g. 2024-2027")
		source  = fs.String("source", "", "fetch from a single source only")
		replace = fs.Bool("replace", false, "delete the year's existing rows first (use when dates have moved)")
		dbPath  = dbFlag(fs)
	)
	fs.Parse(args)

	years, err := targetYears(*year, *yearRng)
	if err != nil {
		return err
	}

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	reg := sources.NewRegistry(httpx.New())

	selected := reg.All()
	if *source != "" {
		s, ok := reg.Get(*source)
		if !ok {
			return fmt.Errorf("unknown source %q; available: %v", *source, reg.Names())
		}
		selected = []sources.Source{s}
	}

	for _, y := range years {
		if err := scrapeYear(ctx, st, selected, y, *replace); err != nil {
			return err
		}
	}
	return nil
}

// scrapeYear fetches every source for one year, reconciles, and persists.
func scrapeYear(ctx context.Context, st *store.Store, srcs []sources.Source, year int, replace bool) error {
	fmt.Printf("\n=== %d\n", year)

	snaps := []*model.Snapshot{}

	for _, s := range srcs {
		snap, err := s.Fetch(ctx, year)

		rec := store.FetchRecord{
			Year:       year,
			Source:     s.Name(),
			Confidence: s.Authority(),
			FetchedAt:  time.Now().UTC(),
		}

		switch {
		case err != nil && sources.Expected(err):
			rec.OK = false
			rec.Note = err.Error()
			fmt.Printf("  %-10s skipped: %v\n", s.Name(), err)
		case err != nil:
			rec.OK = false
			rec.Note = err.Error()
			fmt.Printf("  %-10s FAILED:  %v\n", s.Name(), err)
		default:
			rec.OK = true
			rec.SourceURL = snap.SourceURL
			rec.Decree = snap.Decree
			rec.DayCount = len(snap.Holidays)
			rec.Note = snap.Note

			switch {
			case snap.EvidenceOnly():
				detail := []string{}
				if snap.AnnouncedDays > 0 {
					detail = append(detail, fmt.Sprintf("announces %d days", snap.AnnouncedDays))
				}
				if snap.Decree != "" {
					detail = append(detail, snap.Decree)
				}
				if snap.DocumentURL != "" {
					detail = append(detail, snap.DocumentURL)
				}
				fmt.Printf("  %-10s evidence: %s\n", s.Name(), joinNonEmpty(detail, "; "))
			default:
				fmt.Printf("  %-10s %d days\n", s.Name(), len(snap.Holidays))
			}
			snaps = append(snaps, snap)
		}

		if err := st.RecordFetch(ctx, rec); err != nil {
			return err
		}
	}

	if len(snaps) == 0 {
		fmt.Printf("  -> no source returned data for %d\n", year)
		return nil
	}

	rec := sources.Reconcile(year, snaps)
	if len(rec.Holidays) == 0 {
		fmt.Printf("  -> only evidence collected for %d; no dates to store\n", year)
		if rec.Decree != "" {
			fmt.Printf("     governing document: %s %s\n", rec.Decree, rec.DocumentURL)
		}
		return nil
	}

	if replace {
		n, err := st.DeleteYear(ctx, year)
		if err != nil {
			return err
		}
		fmt.Printf("  -> removed %d existing row(s) for %d\n", n, year)
	}

	ins, upd, skip, err := st.Upsert(ctx, rec.Holidays)
	if err != nil {
		return err
	}

	fmt.Printf("  -> %d holidays: %d new, %d updated, %d unchanged\n",
		len(rec.Holidays), ins, upd, skip)

	if rec.AnnouncedDays > 0 {
		mark := "MISMATCH"
		if rec.CountMatches {
			mark = "matches"
		}
		fmt.Printf("  -> official count %d vs stored %d: %s\n",
			rec.AnnouncedDays, len(rec.Holidays), mark)
	}
	if rec.Decree != "" {
		fmt.Printf("  -> decree: %s\n", rec.Decree)
	}
	for _, w := range rec.Warnings {
		fmt.Printf("  !  %s\n", w)
	}
	return nil
}

func cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	dbPath := dbFlag(fs)
	fs.Parse(args)

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	years, err := st.Status(ctx)
	if err != nil {
		return err
	}
	if len(years) == 0 {
		fmt.Println("database is empty - run `khapi-scrape scrape` first")
		return nil
	}

	fmt.Printf("%-6s %-6s %-9s %-9s %-9s %-8s %s\n",
		"YEAR", "DAYS", "OFFICIAL", "REPORTED", "COMPUTED", "STATE", "DECREE")
	for _, y := range years {
		state := "confirmed"
		if y.Provisional {
			state = "provisional"
		}
		fmt.Printf("%-6d %-6d %-9d %-9d %-9d %-8s %s\n",
			y.Year, y.Days, y.Official, y.Reported, y.Computed, state, y.Decree)
	}

	fetches, err := st.Fetches(ctx, 0)
	if err != nil {
		return err
	}
	if len(fetches) > 0 {
		fmt.Printf("\n%-6s %-11s %-4s %-6s %s\n", "YEAR", "SOURCE", "OK", "DAYS", "LAST RUN / NOTE")
		for _, f := range fetches {
			ok := "no"
			if f.OK {
				ok = "yes"
			}
			note := f.Note
			if len(note) > 60 {
				note = note[:57] + "..."
			}
			fmt.Printf("%-6d %-11s %-4s %-6d %s  %s\n",
				f.Year, f.Source, ok, f.DayCount,
				f.FetchedAt.Format("2006-01-02"), note)
		}
	}
	return nil
}

// cmdVerify is the human-in-the-loop step. The governing Prakas is published as
// a scanned PDF with no text layer, so no scraper can honestly mark a year
// "official" on its own. An operator reads the document, then records that fact
// here - which locks the dates against being overwritten by later projections.
func cmdVerify(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	var (
		year   = fs.Int("year", 0, "year to mark as officially confirmed")
		decree = fs.String("decree", "", `decree reference, e.g. "Sub-Decree No. 167"`)
		url    = fs.String("url", "", "URL of the document you verified against")
		dbPath = dbFlag(fs)
	)
	fs.Parse(args)

	if *year == 0 {
		return fmt.Errorf("-year is required")
	}
	if *decree == "" {
		return fmt.Errorf("-decree is required: record which document you checked")
	}

	st, err := store.Open(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	hs, err := st.List(ctx, store.Filter{Year: *year})
	if err != nil {
		return err
	}
	if len(hs) == 0 {
		return fmt.Errorf("no holidays stored for %d - scrape it first", *year)
	}

	promoted := sources.Promote(hs, model.ConfidenceOfficial, *decree, *url)
	if _, _, _, err := st.Upsert(ctx, promoted); err != nil {
		return err
	}

	fmt.Printf("marked %d holiday(s) in %d as official under %s\n", len(promoted), *year, *decree)
	if *url != "" {
		fmt.Printf("source document: %s\n", *url)
	}
	return nil
}

func cmdSources(args []string) error {
	fs := flag.NewFlagSet("sources", flag.ExitOnError)
	fs.Parse(args)

	reg := sources.NewRegistry(httpx.New())
	fmt.Printf("%-11s %-10s %s\n", "NAME", "AUTHORITY", "ROLE")
	for _, s := range reg.All() {
		fmt.Printf("%-11s %-10s %s\n", s.Name(), s.Authority(), sourceRole(s.Name()))
	}
	return nil
}

func sourceRole(name string) string {
	switch name {
	case "nager":
		return "primary dates, incl. future-year projections"
	case "wikipedia":
		return "fixed-date cross-check and Khmer names"
	case "akp":
		return "state news agency: announced day count + decree"
	case "mlvt":
		return "Ministry of Labour Prakas (scanned PDF, evidence only)"
	case "mef":
		return "Ministry of Finance (HTTP 403: Cloudflare-blocked)"
	}
	return ""
}

// targetYears resolves the -year / -years flags, defaulting to the current and
// next year - the pair that matters for a cron job watching for the next
// sub-decree.
func targetYears(year int, rng string) ([]int, error) {
	switch {
	case rng != "":
		var lo, hi int
		if _, err := fmt.Sscanf(rng, "%d-%d", &lo, &hi); err != nil {
			return nil, fmt.Errorf("invalid -years %q, want e.g. 2024-2027", rng)
		}
		if hi < lo {
			return nil, fmt.Errorf("invalid -years %q: end is before start", rng)
		}
		if hi-lo > 30 {
			return nil, fmt.Errorf("invalid -years %q: range is too wide", rng)
		}
		out := make([]int, 0, hi-lo+1)
		for y := lo; y <= hi; y++ {
			out = append(out, y)
		}
		return out, nil

	case year != 0:
		if year < 1900 || year > 2200 {
			return nil, fmt.Errorf("implausible year %d", year)
		}
		return []int{year}, nil

	default:
		now := time.Now().Year()
		return []int{now, now + 1}, nil
	}
}

func joinNonEmpty(parts []string, sep string) string {
	out := ""
	for _, p := range parts {
		if p == "" {
			continue
		}
		if out != "" {
			out += sep
		}
		out += p
	}
	if out == "" {
		return "(no detail)"
	}
	return out
}
