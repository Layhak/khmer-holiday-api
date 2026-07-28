package sources

import (
	"sort"
	"strings"
	"time"

	"github.com/layhak/khmer-holiday-api/internal/model"
)

// canonical maps a holiday key to its English and Khmer names. Sources disagree
// on wording ("King's Birthday" vs "Birthday of King Norodom Sihamoni"), so
// every adapter resolves to a key and we render names from here. That keeps the
// API consistent no matter which source last won reconciliation.
var canonical = map[string]struct{ en, km string }{
	"intl_new_year":    {"International New Year's Day", "ទិវាបុណ្យចូលឆ្នាំសកល"},
	"victory_genocide": {"Victory over Genocide Day", "ទិវាជ័យជម្នះលើរបបប្រល័យពូជសាសន៍"},
	"meak_bochea":      {"Meak Bochea Day", "ពិធីបុណ្យមាឃបូជា"},
	"womens_day":       {"International Women's Day", "ទិវានារីអន្តរជាតិ"},
	"khmer_new_year":   {"Khmer New Year", "ពិធីបុណ្យចូលឆ្នាំថ្មីប្រពៃណីជាតិ"},
	"labour_day":       {"International Labour Day", "ទិវាពលកម្មអន្តរជាតិ"},
	"visak_bochea":     {"Visak Bochea Day", "ពិធីបុណ្យវិសាខបូជា"},
	"royal_ploughing":  {"Royal Ploughing Ceremony", "ព្រះរាជពិធីច្រត់ព្រះនង្គ័ល"},
	"king_birthday":    {"Birthday of King Norodom Sihamoni", "ព្រះរាជពិធីបុណ្យចម្រើនព្រះជន្ម ព្រះករុណា នរោត្តម សីហមុនី"},
	"queen_birthday":   {"Birthday of Queen Mother Norodom Monineath", "ព្រះរាជពិធីបុណ្យចម្រើនព្រះជន្ម សម្តេចព្រះមហាក្សត្រី នរោត្តម មុនិនាថ សីហនុ"},
	"constitution_day": {"Constitution Day", "ទិវាប្រកាសរដ្ឋធម្មនុញ្ញ"},
	"pchum_ben":        {"Pchum Ben Festival", "ពិធីបុណ្យភ្ជុំបិណ្ឌ"},
	"kings_father":     {"Commemoration Day of King Father Norodom Sihanouk", "ព្រះរាជពិធីគោរពព្រះវិញ្ញាណក្ខន្ធ ព្រះករុណា ព្រះបាទសម្តេចព្រះ នរោត្តម សីហនុ"},
	"paris_agreement":  {"Paris Peace Agreement Day", "ទិវារំលឹកសន្ធិសញ្ញាសន្តិភាពទីក្រុងប៉ារីស"},
	"coronation_day":   {"Coronation Day of King Norodom Sihamoni", "ព្រះរាជពិធីគ្រងព្រះបរមរាជសម្បត្តិ ព្រះករុណា ព្រះបាទសម្តេចព្រះបរមនាថ នរោត្តម សីហមុនី"},
	"independence_day": {"National Independence Day", "ទិវាបុណ្យឯករាជ្យជាតិ"},
	"water_festival":   {"Water Festival", "ពិធីបុណ្យអុំទូក បណ្តែតប្រទីប អកអំបុក និងសំពះព្រះខែ"},
	"human_rights_day": {"International Human Rights Day", "ទិវាសិទ្ធិមនុស្សអន្តរជាតិ"},
	"peace_day":        {"Cambodia Peace Day", "ទិវាសន្តិភាពនៅកម្ពុជា"},
}

// matchers maps lowercase substrings to a canonical key. Order matters: the
// first entry whose substring appears in the source's name wins, so more
// specific patterns are listed before the generic ones they would shadow.
var matchers = []struct {
	needle string
	key    string
}{
	{"genocide", "victory_genocide"},
	{"meak bochea", "meak_bochea"},
	{"magha", "meak_bochea"},
	{"women", "womens_day"},
	{"khmer new year", "khmer_new_year"},
	{"cambodian new year", "khmer_new_year"},
	{"choul chnam", "khmer_new_year"},
	{"labour", "labour_day"},
	{"labor day", "labour_day"},
	{"visak", "visak_bochea"},
	{"vesak", "visak_bochea"},
	{"ploughing", "royal_ploughing"},
	{"plowing", "royal_ploughing"},
	{"queen", "queen_birthday"},
	{"king's father", "kings_father"},
	{"kings father", "kings_father"},
	{"king father", "kings_father"},
	{"sihanouk", "kings_father"},
	{"coronation", "coronation_day"},
	{"sihamoni", "king_birthday"},
	{"king", "king_birthday"}, // after sihanouk/coronation/queen
	{"constitution", "constitution_day"},
	{"pchum", "pchum_ben"},
	{"ancestor", "pchum_ben"},
	{"paris peace", "paris_agreement"},
	{"human rights", "human_rights_day"},
	{"independence", "independence_day"},
	{"water festival", "water_festival"},
	{"bon om", "water_festival"},
	{"boat racing", "water_festival"},
	{"peace day", "peace_day"},
	{"peace in cambodia", "peace_day"},
	{"win-win", "peace_day"},
	{"new year", "intl_new_year"}, // last: khmer new year is matched above
}

// CanonKey resolves a source's holiday label to a stable key. An unrecognised
// name falls back to a slug of itself, so a newly created holiday still lands
// in the database instead of being dropped.
func CanonKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, m := range matchers {
		if strings.Contains(n, m.needle) {
			return m.key
		}
	}
	return slug(n)
}

// CanonNames returns the canonical English and Khmer names for a key, falling
// back to the source-supplied names when the key is unknown.
func CanonNames(key, fallbackEN, fallbackKM string) (string, string) {
	if c, ok := canonical[key]; ok {
		km := c.km
		if km == "" {
			km = fallbackKM
		}
		return c.en, km
	}
	return fallbackEN, fallbackKM
}

func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevDash = false
		default:
			if !prevDash && b.Len() > 0 {
				b.WriteByte('_')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "_")
}

// GroupMultiDay fills Ordinal and OfDays for festivals that span consecutive
// days, so the API can say "day 2 of 3 of Pchum Ben". Days are grouped by key
// and must be calendar-consecutive to count as one run.
func GroupMultiDay(hs []model.Holiday) []model.Holiday {
	byKey := map[string][]int{} // key -> indices into hs
	for i, h := range hs {
		byKey[h.Key] = append(byKey[h.Key], i)
	}

	for _, idxs := range byKey {
		sort.Slice(idxs, func(a, b int) bool {
			return hs[idxs[a]].Date.Before(hs[idxs[b]].Date)
		})

		// Split into runs of consecutive days.
		run := []int{}
		flush := func() {
			for pos, i := range run {
				hs[i].Ordinal = pos + 1
				hs[i].OfDays = len(run)
			}
			run = nil
		}
		for _, i := range idxs {
			if len(run) > 0 {
				prev := hs[run[len(run)-1]].Date
				if !hs[i].Date.Equal(prev.AddDate(0, 0, 1)) {
					flush()
				}
			}
			run = append(run, i)
		}
		flush()
	}
	return hs
}

// Normalize canonicalises names, flags lunar holidays, groups multi-day
// festivals and sorts by date. Every adapter ends with this so its output
// obeys the same invariants.
func Normalize(hs []model.Holiday) []model.Holiday {
	for i := range hs {
		hs[i].Date = time.Date(hs[i].Date.Year(), hs[i].Date.Month(), hs[i].Date.Day(),
			0, 0, 0, 0, time.UTC)
		hs[i].NameEN, hs[i].NameKM = CanonNames(hs[i].Key, hs[i].NameEN, hs[i].NameKM)
		hs[i].IsLunar = model.Lunar(hs[i].Key)
	}
	hs = GroupMultiDay(hs)
	sort.Slice(hs, func(a, b int) bool { return hs[a].Date.Before(hs[b].Date) })
	return hs
}
