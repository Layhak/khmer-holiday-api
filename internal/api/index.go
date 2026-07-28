package api

import (
	"net/http"
)

// handleIndex serves a short, self-contained API reference at the root so the
// service documents itself without an external spec file.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found: "+r.URL.Path)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(indexHTML))
}

const indexHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Cambodia Public Holiday API</title>
<style>
:root{color-scheme:light dark;--fg:#1a1a1a;--bg:#fff;--mut:#666;--acc:#0b6;--bd:#e3e3e3;--cd:#f6f6f6}
@media(prefers-color-scheme:dark){:root{--fg:#e8e8e8;--bg:#161616;--mut:#9a9a9a;--bd:#333;--cd:#1f1f1f}}
*{box-sizing:border-box}
body{margin:0;padding:2rem 1.25rem;font:16px/1.6 system-ui,-apple-system,sans-serif;color:var(--fg);background:var(--bg)}
main{max-width:56rem;margin:0 auto}
h1{font-size:1.6rem;margin:0 0 .25rem}
h2{font-size:1.05rem;margin:2rem 0 .6rem;padding-bottom:.3rem;border-bottom:1px solid var(--bd)}
.sub{color:var(--mut);margin:0 0 1.5rem}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.875em}
pre{background:var(--cd);border:1px solid var(--bd);border-radius:6px;padding:.75rem .9rem;overflow-x:auto}
code:not(pre code){background:var(--cd);padding:.1em .35em;border-radius:3px}
table{border-collapse:collapse;width:100%;font-size:.9rem}
th,td{text-align:left;padding:.45rem .6rem;border-bottom:1px solid var(--bd);vertical-align:top}
th{color:var(--mut);font-weight:600}
.w{background:#fff4e5;border-left:3px solid #e8a33d;padding:.75rem .9rem;border-radius:0 6px 6px 0;font-size:.92rem}
@media(prefers-color-scheme:dark){.w{background:#2a2010}}
.tag{display:inline-block;font-size:.72rem;padding:.1em .5em;border-radius:99px;border:1px solid var(--bd);color:var(--mut)}
a{color:var(--acc)}
.scroll{overflow-x:auto}
</style></head><body><main>
<h1>Cambodia Public Holiday API</h1>
<p class="sub">Filter Cambodian public holidays by day, month and year. Every record carries its source and confidence.</p>

<div class="w"><strong>Confidence matters.</strong> Cambodian holidays are fixed each year by a sub-decree signed around September of the preceding year. Lunar holidays &mdash; Pchum Ben, Water Festival, Royal Ploughing &mdash; are <em>projections</em> until that decree exists. Check the <code>confidence</code> field, or pass <code>?official=true</code>.</p></div>

<h2>Endpoints</h2>
<div class="scroll"><table>
<tr><th>Route</th><th>Purpose</th></tr>
<tr><td><code>GET /api/v1/holidays</code></td><td>List and filter holidays</td></tr>
<tr><td><code>GET /api/v1/holidays/{date}</code></td><td>Is <code>YYYY-MM-DD</code> a holiday?</td></tr>
<tr><td><code>GET /api/v1/years</code></td><td>Years held in the database</td></tr>
<tr><td><code>GET /api/v1/status</code></td><td>Coverage and per-source scrape audit</td></tr>
<tr><td><code>GET /api/v1/sources</code></td><td>Upstream sources and their status</td></tr>
<tr><td><code>GET /healthz</code></td><td>Liveness</td></tr>
</table></div>

<h2>Filters</h2>
<div class="scroll"><table>
<tr><th>Param</th><th>Example</th><th>Meaning</th></tr>
<tr><td><code>year</code></td><td>2026</td><td>Calendar year</td></tr>
<tr><td><code>month</code></td><td>4</td><td>Month, 1&ndash;12</td></tr>
<tr><td><code>day</code></td><td>14</td><td>Day of month, 1&ndash;31</td></tr>
<tr><td><code>from</code>, <code>to</code></td><td>2026-01-01</td><td>Inclusive date range</td></tr>
<tr><td><code>key</code></td><td>pchum_ben</td><td>One holiday series</td></tr>
<tr><td><code>official</code></td><td>true</td><td>Only decree-confirmed dates</td></tr>
</table></div>
<p>Filters combine with AND, so <code>?month=4&amp;day=14</code> returns 14 April across every stored year.</p>

<h2>Examples</h2>
<pre>curl 'http://localhost:8080/api/v1/holidays?year=2026'
curl 'http://localhost:8080/api/v1/holidays?year=2026&amp;month=4'
curl 'http://localhost:8080/api/v1/holidays/2026-04-14'
curl 'http://localhost:8080/api/v1/holidays?key=pchum_ben'
curl 'http://localhost:8080/api/v1/holidays?year=2027&amp;official=true'</pre>

<h2>Response</h2>
<pre>{
  "count": 3,
  "filter": { "year": 2026, "month": 4 },
  "warnings": [ "3 of the returned 2026 holidays are not yet confirmed..." ],
  "holidays": [
    {
      "key": "khmer_new_year",
      "name_en": "Khmer New Year",
      "name_km": "&#4796;&#4636;&#4792;&#4770;&#4794;&#4784;&#4784;&#4753;&#4791;&#4785;&#4771;&#4780;&#4816;&#4813;&#4786;&#4796;&#4785;&#4784;",
      "ordinal": 1, "of_days": 3,
      "is_lunar": false,
      "confidence": "reported",
      "source": "nager",
      "decree": "Sub-Decree No. 167",
      "date": "2026-04-14",
      "year": 2026, "month": 4, "day": 14,
      "weekday": "Tuesday"
    }
  ]
}</pre>

<h2>Confidence levels</h2>
<div class="scroll"><table>
<tr><th>Value</th><th>Meaning</th></tr>
<tr><td><span class="tag">official</span></td><td>Verified against the signed sub-decree or Prakas.</td></tr>
<tr><td><span class="tag">reported</span></td><td>Dates corroborated by the state news agency's announced day count.</td></tr>
<tr><td><span class="tag">computed</span></td><td>Projected from the lunisolar calendar. May shift.</td></tr>
</table></div>
</main></body></html>`
