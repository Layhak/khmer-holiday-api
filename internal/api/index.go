package api

import (
	"bytes"
	_ "embed"
	"net/http"
	"time"
)

const publicBaseURL = "https://khmerholiday.layhak.dev"

//go:embed assets/aba-khqr.png
var donationQR []byte

//go:embed assets/social-preview.png
var socialPreview []byte

//go:embed assets/site.js
var siteJS []byte

// handleIndex serves a self-contained API reference and landing page at the
// root. It deliberately has no JavaScript or third-party resources.
func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not found: "+r.URL.Path)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(indexHTML))
}

func (s *Server) handleDonationQR(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `inline; filename="layhak-heng-aba-khqr.png"`)
	http.ServeContent(w, r, "layhak-heng-aba-khqr.png", time.Time{}, bytes.NewReader(donationQR))
}

func (s *Server) handleSocialPreview(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Content-Disposition", `inline; filename="cambodia-holiday-api.png"`)
	http.ServeContent(w, r, "cambodia-holiday-api.png", time.Time{}, bytes.NewReader(socialPreview))
}

func (s *Server) handleSiteJS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	_, _ = w.Write(siteJS)
}

func (s *Server) handleRobots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(robotsTXT))
}

func (s *Server) handleSitemap(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	_, _ = w.Write([]byte(sitemapXML))
}

const robotsTXT = `User-agent: *
Allow: /
Disallow: /api/
Disallow: /healthz

Sitemap: https://khmerholiday.layhak.dev/sitemap.xml
`

const sitemapXML = `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
  <url>
    <loc>https://khmerholiday.layhak.dev/</loc>
    <changefreq>weekly</changefreq>
    <priority>1.0</priority>
  </url>
</urlset>
`

const indexHTML = `<!doctype html>
<html lang="en-KH">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>Cambodia Public Holidays &amp; Free Khmer Holiday API</title>
<meta name="description" content="Current and upcoming Cambodia public holidays with Khmer and English names, source confidence, date filters and a free JSON API for developers.">
<meta name="robots" content="index,follow,max-image-preview:large">
<meta name="theme-color" content="#075985">
<link rel="canonical" href="https://khmerholiday.layhak.dev/">
<meta property="og:type" content="website">
<meta property="og:site_name" content="Khmer Holiday API">
<meta property="og:title" content="Cambodia Public Holidays &amp; Free Khmer Holiday API">
<meta property="og:description" content="Cambodian public holiday dates in Khmer and English, with transparent sources and a free JSON API.">
<meta property="og:url" content="https://khmerholiday.layhak.dev/">
<meta property="og:image" content="https://khmerholiday.layhak.dev/assets/social-preview.png">
<meta property="og:image:secure_url" content="https://khmerholiday.layhak.dev/assets/social-preview.png">
<meta property="og:image:type" content="image/png">
<meta property="og:image:width" content="1200">
<meta property="og:image:height" content="630">
<meta property="og:image:alt" content="Cambodia Holiday API official Khmer and English public holiday data">
<meta property="og:locale" content="en_KH">
<meta property="og:locale:alternate" content="km_KH">
<meta name="twitter:card" content="summary_large_image">
<meta name="twitter:title" content="Cambodia Public Holidays &amp; Free Khmer Holiday API">
<meta name="twitter:description" content="Cambodian public holiday dates in Khmer and English, with transparent sources and a free JSON API.">
<meta name="twitter:image" content="https://khmerholiday.layhak.dev/assets/social-preview.png">
<meta name="twitter:image:alt" content="Cambodia Holiday API official Khmer and English public holiday data">
<style>
:root{color-scheme:light dark;--fg:#17202a;--bg:#f8fafc;--surface:#fff;--mut:#5f6b78;--acc:#0369a1;--acc2:#0f766e;--bd:#dbe4ea;--code:#f1f5f9;--warn:#fff7e6;--warnbd:#e8a33d;--donate:#f0f9ff;--shadow:0 16px 44px rgba(15,23,42,.08)}
@media(prefers-color-scheme:dark){:root{--fg:#e8edf2;--bg:#0d151c;--surface:#141f28;--mut:#a4b0bb;--acc:#38bdf8;--acc2:#5eead4;--bd:#2a3b48;--code:#192630;--warn:#2a2112;--donate:#102531;--shadow:none}}
*{box-sizing:border-box}
html{scroll-behavior:smooth}
body{margin:0;font:16px/1.65 system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans Khmer",sans-serif;color:var(--fg);background:var(--bg)}
a{color:var(--acc)}
.skip{position:absolute;left:-9999px;top:auto}.skip:focus{left:1rem;top:1rem;background:var(--surface);padding:.5rem;z-index:2}
header{background:linear-gradient(135deg,#075985,#0f766e);color:#fff;padding:3.6rem 1.25rem 3rem}
.hero,main,footer{max-width:64rem;margin:0 auto}
.eyebrow{display:inline-block;margin-bottom:.8rem;padding:.2rem .65rem;border:1px solid rgba(255,255,255,.4);border-radius:999px;font-size:.78rem;letter-spacing:.06em;text-transform:uppercase}
h1{max-width:48rem;font-size:clamp(2rem,5vw,3.35rem);line-height:1.1;margin:0 0 1rem;letter-spacing:-.035em}
.lead{max-width:45rem;margin:0;color:#e6f7ff;font-size:1.1rem}
.khmer{font-family:"Noto Sans Khmer",system-ui,sans-serif}
nav{margin-top:1.4rem;display:flex;gap:.7rem;flex-wrap:wrap}
nav a{color:#fff;text-decoration:none;border-bottom:1px solid rgba(255,255,255,.55)}
.github-link{padding:.1rem .65rem;border:1px solid rgba(255,255,255,.65)!important;border-radius:999px;background:rgba(255,255,255,.1);font-weight:700}
.github-link:hover{background:rgba(255,255,255,.2)}
main{padding:2.2rem 1.25rem 4rem}
.facts{display:grid;grid-template-columns:repeat(3,1fr);gap:1rem;margin-top:-3.5rem;margin-bottom:2.5rem}
.fact{background:var(--surface);border:1px solid var(--bd);border-radius:12px;padding:1rem 1.1rem;box-shadow:var(--shadow)}
.fact strong{display:block;color:var(--acc2);font-size:1.05rem}
h2{font-size:1.35rem;margin:2.5rem 0 .75rem;padding-bottom:.35rem;border-bottom:1px solid var(--bd)}
h3{font-size:1rem;margin:0 0 .35rem}
.sub{color:var(--mut);margin:.1rem 0 1.5rem}
.w{background:var(--warn);border-left:3px solid var(--warnbd);padding:.85rem 1rem;border-radius:0 8px 8px 0;font-size:.94rem}
code,pre{font-family:ui-monospace,SFMono-Regular,Menlo,monospace;font-size:.875em}
pre{background:var(--code);border:1px solid var(--bd);border-radius:8px;padding:.9rem 1rem;overflow-x:auto}
code:not(pre code){background:var(--code);padding:.1em .35em;border-radius:3px}
button{font:inherit}
table{border-collapse:collapse;width:100%;font-size:.9rem}
th,td{text-align:left;padding:.5rem .65rem;border-bottom:1px solid var(--bd);vertical-align:top}
th{color:var(--mut);font-weight:650}
.tag{display:inline-block;font-size:.72rem;padding:.1em .5em;border-radius:99px;border:1px solid var(--bd);color:var(--mut)}
.scroll{overflow-x:auto}
.examples{display:grid;gap:1rem;min-width:0}
.example{min-width:0;background:var(--surface);border:1px solid var(--bd);border-radius:12px;padding:1rem;box-shadow:var(--shadow)}
.example-head,.code-head{display:flex;align-items:center;justify-content:space-between;gap:1rem}
.example-head p{margin:.15rem 0;color:var(--mut);font-size:.9rem}
.code-head{margin-top:.85rem;color:var(--mut);font-size:.78rem;font-weight:650;text-transform:uppercase;letter-spacing:.04em}
.example pre{width:100%;max-width:100%;margin:.35rem 0 0;max-height:28rem}
.copy{flex:0 0 auto;border:1px solid var(--bd);border-radius:7px;padding:.35rem .65rem;color:var(--fg);background:var(--code);cursor:pointer;font-size:.82rem;font-weight:650}
.copy:hover{border-color:var(--acc);color:var(--acc)}
.copy:focus-visible{outline:3px solid color-mix(in srgb,var(--acc) 40%,transparent);outline-offset:2px}
.copy:disabled{cursor:not-allowed;opacity:.55}
.live-response{margin-top:.9rem;border-top:1px solid var(--bd);padding-top:.75rem}
.live-response summary{cursor:pointer;color:var(--acc);font-weight:650}
.live-response[open] summary{margin-bottom:.65rem}
.response-status{min-height:1.3em;margin:.45rem 0 0;color:var(--mut);font-size:.82rem}
.response-status.error{color:#b42318}
.donate{display:grid;grid-template-columns:minmax(0,1fr) minmax(260px,390px);gap:2rem;align-items:center;margin-top:3rem;padding:clamp(1.2rem,4vw,2.2rem);background:var(--donate);border:1px solid #bae6fd;border-radius:18px}
@media(prefers-color-scheme:dark){.donate{border-color:#164e63}}
.donate h2{margin:0 0 .7rem;border:0;padding:0;font-size:1.55rem}
.donate ul{padding-left:1.2rem}
.qr{display:block;width:100%;height:auto;border-radius:12px;border:1px solid var(--bd);background:#fff}
.verify{font-size:.88rem;color:var(--mut)}
footer{padding:1.25rem;color:var(--mut);font-size:.88rem;border-top:1px solid var(--bd)}
@media(max-width:720px){header{padding-top:2.6rem}.facts{grid-template-columns:1fr;margin-top:-2rem}.example-head{align-items:flex-start}.donate{grid-template-columns:1fr}.donate picture{max-width:420px;margin:0 auto}table{min-width:34rem}}
</style>
</head>
<body>
<a class="skip" href="#content">Skip to content</a>
<header>
  <div class="hero">
    <span class="eyebrow">Free public JSON API</span>
    <h1>Cambodia Public Holidays &amp; Khmer Holiday API</h1>
    <p class="lead">Search Cambodian public holidays by date, month or year, with Khmer and English names and a clear confidence level for every result.</p>
    <p class="lead khmer" lang="km">ស្វែងរកថ្ងៃឈប់សម្រាកសាធារណៈនៅកម្ពុជា ជាភាសាខ្មែរ និងអង់គ្លេស។</p>
    <nav aria-label="Page navigation">
      <a href="#endpoints">API endpoints</a>
      <a href="#examples">Examples</a>
      <a href="#confidence">Data confidence</a>
      <a href="#support">Support this project</a>
      <a class="github-link" href="https://github.com/Layhak/khmer-holiday-api" target="_blank" rel="noopener noreferrer" aria-label="Star Khmer Holiday API on GitHub">★ Star on GitHub</a>
    </nav>
  </div>
</header>

<main id="content" itemscope itemtype="https://schema.org/WebAPI">
<meta itemprop="name" content="Khmer Holiday API">
<meta itemprop="description" content="Free JSON API for Cambodia public holiday dates in Khmer and English.">
<link itemprop="url" href="https://khmerholiday.layhak.dev/">

<div class="facts" aria-label="Service highlights">
  <div class="fact"><strong>Khmer + English</strong>Localized public holiday names</div>
  <div class="fact"><strong>Transparent sources</strong>Official, reported or computed confidence</div>
  <div class="fact"><strong>Free JSON API</strong>Simple filters for apps and websites</div>
</div>

<div class="w"><strong>Confidence matters.</strong> Cambodia fixes its annual public holidays through a sub-decree or Prakas. Lunar dates such as Pchum Ben, Water Festival and Royal Ploughing may remain projections until the governing document is verified. Check <code>confidence</code>, or pass <code>?official=true</code>.</div>

<section id="endpoints">
<h2>API endpoints</h2>
<p class="sub">Use the API from calendars, HR systems, payroll software and Cambodia-focused applications.</p>
<div class="scroll"><table>
<tr><th>Route</th><th>Purpose</th></tr>
<tr><td><code>GET /api/v1/holidays</code></td><td>List and filter Cambodia holidays</td></tr>
<tr><td><code>GET /api/v1/holidays/{date}</code></td><td>Check whether <code>YYYY-MM-DD</code> is a holiday</td></tr>
<tr><td><code>GET /api/v1/years</code></td><td>List years held in the database</td></tr>
<tr><td><code>GET /api/v1/status</code></td><td>View coverage and source audit status</td></tr>
<tr><td><code>GET /api/v1/sources</code></td><td>View upstream sources</td></tr>
<tr><td><code>GET /healthz</code></td><td>Service liveness</td></tr>
</table></div>
</section>

<section>
<h2>Holiday filters</h2>
<div class="scroll"><table>
<tr><th>Parameter</th><th>Example</th><th>Meaning</th></tr>
<tr><td><code>year</code></td><td>2026</td><td>Calendar year</td></tr>
<tr><td><code>month</code></td><td>4</td><td>Month, 1&ndash;12</td></tr>
<tr><td><code>day</code></td><td>14</td><td>Day of month, 1&ndash;31</td></tr>
<tr><td><code>from</code>, <code>to</code></td><td>2026-01-01</td><td>Inclusive date range</td></tr>
<tr><td><code>key</code></td><td>pchum_ben</td><td>One holiday series</td></tr>
<tr><td><code>official</code></td><td>true</td><td>Only dates verified against the governing document</td></tr>
</table></div>
<p>Filters combine with AND. For example, <code>?month=4&amp;day=14</code> returns 14 April across every stored year.</p>
</section>

<section id="examples">
<h2>Examples</h2>
<p class="sub">Each example is separate. Copy its request, then open the live response to run it against the current dataset.</p>
<div class="examples">
  <article class="example" data-endpoint="/api/v1/holidays?year=2026">
    <div class="example-head">
      <div><h3>Holidays by year</h3><p>Return every stored Cambodian holiday for 2026.</p></div>
      <button class="copy" type="button" data-copy-target="request-year">Copy request</button>
    </div>
    <pre id="request-year"><code>curl 'https://khmerholiday.layhak.dev/api/v1/holidays?year=2026'</code></pre>
    <details class="live-response" open>
      <summary>Live response</summary>
      <div class="code-head"><span>JSON response</span><button class="copy response-copy" type="button" data-copy-target="response-year" disabled>Copy response</button></div>
      <pre id="response-year"><code>Loading current response…</code></pre>
      <p class="response-status" role="status" aria-live="polite"></p>
    </details>
  </article>

  <article class="example" data-endpoint="/api/v1/holidays?year=2026&amp;month=4">
    <div class="example-head">
      <div><h3>Holidays by year and month</h3><p>Combine filters to return April 2026 holidays.</p></div>
      <button class="copy" type="button" data-copy-target="request-month">Copy request</button>
    </div>
    <pre id="request-month"><code>curl 'https://khmerholiday.layhak.dev/api/v1/holidays?year=2026&amp;month=4'</code></pre>
    <details class="live-response">
      <summary>Live response</summary>
      <div class="code-head"><span>JSON response</span><button class="copy response-copy" type="button" data-copy-target="response-month" disabled>Copy response</button></div>
      <pre id="response-month"><code>Open this section to load the current response.</code></pre>
      <p class="response-status" role="status" aria-live="polite"></p>
    </details>
  </article>

  <article class="example" data-endpoint="/api/v1/holidays/2026-04-14">
    <div class="example-head">
      <div><h3>Check one date</h3><p>Find out whether 14 April 2026 is a public holiday.</p></div>
      <button class="copy" type="button" data-copy-target="request-date">Copy request</button>
    </div>
    <pre id="request-date"><code>curl 'https://khmerholiday.layhak.dev/api/v1/holidays/2026-04-14'</code></pre>
    <details class="live-response">
      <summary>Live response</summary>
      <div class="code-head"><span>JSON response</span><button class="copy response-copy" type="button" data-copy-target="response-date" disabled>Copy response</button></div>
      <pre id="response-date"><code>Open this section to load the current response.</code></pre>
      <p class="response-status" role="status" aria-live="polite"></p>
    </details>
  </article>

  <article class="example" data-endpoint="/api/v1/holidays?key=pchum_ben">
    <div class="example-head">
      <div><h3>Find a holiday series</h3><p>Return every stored Pchum Ben date.</p></div>
      <button class="copy" type="button" data-copy-target="request-key">Copy request</button>
    </div>
    <pre id="request-key"><code>curl 'https://khmerholiday.layhak.dev/api/v1/holidays?key=pchum_ben'</code></pre>
    <details class="live-response">
      <summary>Live response</summary>
      <div class="code-head"><span>JSON response</span><button class="copy response-copy" type="button" data-copy-target="response-key" disabled>Copy response</button></div>
      <pre id="response-key"><code>Open this section to load the current response.</code></pre>
      <p class="response-status" role="status" aria-live="polite"></p>
    </details>
  </article>

  <article class="example" data-endpoint="/api/v1/holidays?year=2026&amp;official=true">
    <div class="example-head">
      <div><h3>Official dates only</h3><p>Exclude every date not yet verified against the governing document.</p></div>
      <button class="copy" type="button" data-copy-target="request-official">Copy request</button>
    </div>
    <pre id="request-official"><code>curl 'https://khmerholiday.layhak.dev/api/v1/holidays?year=2026&amp;official=true'</code></pre>
    <details class="live-response">
      <summary>Live response</summary>
      <div class="code-head"><span>JSON response</span><button class="copy response-copy" type="button" data-copy-target="response-official" disabled>Copy response</button></div>
      <pre id="response-official"><code>Open this section to load the current response.</code></pre>
      <p class="response-status" role="status" aria-live="polite"></p>
    </details>
  </article>
</div>
</section>

<section id="confidence">
<h2>Data confidence</h2>
<div class="scroll"><table>
<tr><th>Value</th><th>Meaning</th></tr>
<tr><td><span class="tag">official</span></td><td>Verified against the signed sub-decree or Prakas.</td></tr>
<tr><td><span class="tag">reported</span></td><td>Published by a reputable source and corroborated where possible.</td></tr>
<tr><td><span class="tag">computed</span></td><td>Projected from the lunisolar calendar and may shift.</td></tr>
</table></div>
</section>

<section class="donate" id="support" aria-labelledby="support-heading">
  <div>
    <h2 id="support-heading">Support the Khmer Holiday API</h2>
    <p>This API is free to use. If it saves you time, you can help cover the small server and data-maintenance costs with an optional ABA KHQR donation.</p>
    <p class="khmer" lang="km">API នេះប្រើប្រាស់ដោយឥតគិតថ្លៃ។ ប្រសិនបើវាមានប្រយោជន៍ អ្នកអាចជួយគាំទ្រថ្លៃម៉ាស៊ីនមេ និងការថែទាំទិន្នន័យតាម ABA KHQR។</p>
    <ul>
      <li>Scan using ABA Mobile or a KHQR-supported banking app.</li>
      <li>KHR account: <strong>003 053 536</strong></li>
      <li>USD account: <strong>027 112 002</strong></li>
    </ul>
    <p class="verify"><strong>Before paying:</strong> confirm that the recipient shown in your banking app is <strong>LAYHAK HENG</strong>. Donations are optional and do not unlock extra API access.</p>
  </div>
  <picture>
    <img class="qr" src="/support/aba-khqr.png" width="990" height="1400" loading="lazy" decoding="async" alt="ABA KHQR donation code for Layhak Heng">
  </picture>
</section>
</main>

<footer>
  Cambodia public holiday data for developers and the Cambodian community. Built and maintained by Layhak Heng.
  <a href="https://github.com/Layhak/khmer-holiday-api" target="_blank" rel="noopener noreferrer">View the source and give it a star on GitHub ★</a>
</footer>
<script src="/assets/site.js" defer></script>
</body>
</html>`
