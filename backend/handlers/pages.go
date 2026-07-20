package handlers

import (
	"bytes"
	"html/template"
)

// Inline HTML pages for the public, browser-facing safety surfaces: the
// disabled-link 410 warning and the GET /{code}+ preview. Deliberately
// org-neutral (no deployment branding, no org names — a phisher must not be
// able to borrow trust from these pages), self-contained (inline CSS, no
// asset dependencies — they must render on any custom domain) and tiny.
// html/template auto-escapes every interpolation.

const pageShell = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="robots" content="noindex">
<title>{{.Title}}</title>
<style>
  body { margin: 0; font-family: system-ui, -apple-system, sans-serif; background: #f4f4f5;
         color: #18181b; display: flex; min-height: 100vh; align-items: center; justify-content: center; }
  main { background: #fff; border: 1px solid #e4e4e7; border-radius: 12px; padding: 24px;
         margin: 16px; max-width: 560px; width: 100%; box-sizing: border-box; }
  h1 { font-size: 1.15rem; margin: 0 0 12px; }
  p { font-size: .95rem; line-height: 1.5; margin: 8px 0; }
  .muted { color: #52525b; font-size: .85rem; }
  .dest { word-break: break-all; background: #f4f4f5; border: 1px solid #e4e4e7; border-radius: 8px;
          padding: 10px 12px; font-family: ui-monospace, monospace; font-size: .85rem; margin: 12px 0; }
  .warn { border-left: 4px solid #dc2626; padding-left: 12px; }
  a { color: #4f46e5; }
  form { margin-top: 16px; border-top: 1px solid #e4e4e7; padding-top: 16px; }
  label { display: block; font-size: .8rem; font-weight: 600; margin: 10px 0 4px; }
  input, textarea { width: 100%; box-sizing: border-box; border: 1px solid #d4d4d8; border-radius: 8px;
                    padding: 8px 10px; font-size: .9rem; font-family: inherit; }
  button { margin-top: 12px; background: #18181b; color: #fff; border: 0; border-radius: 8px;
           padding: 9px 16px; font-size: .9rem; cursor: pointer; }
  button:disabled { opacity: .5; cursor: default; }
  .notice { margin-top: 10px; font-size: .85rem; }
  .notice.ok { color: #15803d; }
  .notice.err { color: #b91c1c; }
</style>
</head>
<body>
<main>
{{block "content" .}}{{end}}
</main>
</body>
</html>`

// disabledTmpl is the 410 warning served instead of a redirect for a link
// with status DISABLED_ABUSE. The reason stays coarse: which check, list or
// rule matched is never revealed.
var disabledTmpl = template.Must(newPageTemplate(`{{define "content"}}
<h1 class="warn">This link has been disabled</h1>
<p>The destination behind this short link was flagged by security checks, so it no longer
redirects.</p>
{{if .AbuseContact}}<p class="muted">If you believe this is a mistake, contact
<a href="mailto:{{.AbuseContact}}">{{.AbuseContact}}</a>.</p>{{end}}
{{end}}`))

// previewTmpl is the GET /{code}+ preview: the destination shown as plain
// text (the proceed link is deliberately understated, never a styled
// call-to-action), plus the abuse-report form.
var previewTmpl = template.Must(newPageTemplate(`{{define "content"}}
<h1>Link preview</h1>
<p class="muted">This short link points to the following destination. No visit has been
recorded.</p>
<div class="dest">{{.DestinationURL}}</div>
<p><a href="{{.DestinationURL}}" rel="nofollow noreferrer">Proceed to this destination</a></p>
<form id="report-form">
  <p><strong>Report this link</strong></p>
  <p class="muted">If this link is misleading, harmful or unwanted, let the operators of this
  service know.</p>
  <label for="report-reason">Why are you reporting it? (140 characters max)</label>
  <textarea id="report-reason" rows="3" maxlength="140" required></textarea>
  <label for="report-contact">Your contact (optional)</label>
  <input id="report-contact" type="text" maxlength="255" placeholder="email, in case we have questions">
  <button type="submit">Send report</button>
  <p class="notice" id="report-notice" role="status"></p>
</form>
{{if .AbuseContact}}<p class="muted">Abuse contact for this service:
<a href="mailto:{{.AbuseContact}}">{{.AbuseContact}}</a>.</p>{{end}}
<script>
(function () {
  var form = document.getElementById("report-form");
  var notice = document.getElementById("report-notice");
  form.addEventListener("submit", function (ev) {
    ev.preventDefault();
    var button = form.querySelector("button");
    button.disabled = true;
    notice.className = "notice";
    notice.textContent = "Sending…";
    fetch("/api/v1/report", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        code: {{.Code}},
        reason: document.getElementById("report-reason").value,
        reporter_contact: document.getElementById("report-contact").value
      })
    }).then(function (res) {
      if (res.ok) {
        notice.className = "notice ok";
        notice.textContent = "Thanks — your report was received.";
        form.querySelector("textarea").value = "";
      } else {
        return res.json().then(function (body) {
          throw new Error((body && body.error && body.error.message) || "Report failed.");
        });
      }
    }).catch(function (err) {
      notice.className = "notice err";
      notice.textContent = err.message || "Report failed — please try again.";
      button.disabled = false;
    });
  });
})();
</script>
{{end}}`))

// notFoundTmpl is the browser-facing 404 for an unknown code on the preview
// route.
var notFoundTmpl = template.Must(newPageTemplate(`{{define "content"}}
<h1>No such link</h1>
<p class="muted">There is no short link for this code on this service.</p>
{{end}}`))

// newPageTemplate parses the shared shell plus one content block.
func newPageTemplate(content string) (*template.Template, error) {
	t, err := template.New("page").Parse(pageShell)
	if err != nil {
		return nil, err
	}

	return t.Parse(content)
}

// pageData is the union of what the page templates interpolate.
type pageData struct {
	Title          string
	Code           string
	DestinationURL string
	AbuseContact   string
}

// renderPage executes a page template to bytes; a template failure returns
// a minimal fallback rather than an empty body.
func renderPage(t *template.Template, data pageData) []byte {
	var buf bytes.Buffer

	if err := t.Execute(&buf, data); err != nil {
		return []byte("<!DOCTYPE html><title>" + template.HTMLEscapeString(data.Title) + "</title>")
	}

	return buf.Bytes()
}
