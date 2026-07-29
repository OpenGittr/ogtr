package email

import "html"

// cta is an optional call-to-action button.
type cta struct {
	Label string
	URL   string
}

// render wraps a heading + HTML body in the shared branded shell: an ogtr.in
// logo header (brand indigo), the content, an optional CTA button, and a
// footer. Deliberately email-client-safe — tables, inline styles, a hosted PNG
// logo (SVG/inline logos are stripped by Gmail), no JS. The bodyHTML is trusted
// (built by this package with escaped interpolations); heading/CTA label are
// escaped here.
func render(heading, bodyHTML string, action *cta) string {
	ctaHTML := ""
	if action != nil && action.URL != "" {
		ctaHTML = `<table role="presentation" cellpadding="0" cellspacing="0" style="margin:22px 0 6px;"><tr>` +
			`<td style="border-radius:10px;background:#4f46e5;">` +
			`<a href="` + html.EscapeString(action.URL) + `" style="display:inline-block;padding:11px 22px;` +
			`font-size:14px;font-weight:600;color:#ffffff;text-decoration:none;border-radius:10px;">` +
			html.EscapeString(action.Label) + `</a></td></tr></table>`
	}

	return `<!DOCTYPE html><html><body style="margin:0;padding:0;background:#f1f5f9;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f1f5f9;padding:32px 12px;">` +
		`<tr><td align="center">` +
		`<table role="presentation" width="520" cellpadding="0" cellspacing="0" style="max-width:520px;width:100%;` +
		`background:#ffffff;border:1px solid #e2e8f0;border-radius:14px;overflow:hidden;` +
		`font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;">` +
		// Brand header: logo + ogtr.in wordmark.
		`<tr><td style="padding:22px 28px;border-bottom:1px solid #eef2f7;">` +
		`<table role="presentation" cellpadding="0" cellspacing="0"><tr>` +
		`<td style="padding-right:10px;"><img src="https://ogtr.in/icon-192.png" width="34" height="34" alt="ogtr.in" ` +
		`style="display:block;border-radius:8px;"></td>` +
		`<td style="font-size:20px;font-weight:700;letter-spacing:-0.5px;">` +
		`<span style="color:#4f46e5;">ogtr</span><span style="color:#94a3b8;">.in</span></td>` +
		`</tr></table></td></tr>` +
		// Body.
		`<tr><td style="padding:28px;">` +
		`<h1 style="margin:0 0 12px;font-size:20px;line-height:1.3;color:#0f172a;">` + html.EscapeString(heading) + `</h1>` +
		`<div style="font-size:15px;line-height:1.6;color:#334155;">` + bodyHTML + `</div>` +
		ctaHTML +
		`</td></tr>` +
		// Footer: the domain, linked.
		`<tr><td style="padding:18px 28px;background:#f8fafc;border-top:1px solid #eef2f7;color:#94a3b8;` +
		`font-size:12px;line-height:1.5;">` +
		`<a href="https://ogtr.in" style="color:#6366f1;text-decoration:none;font-weight:600;">ogtr.in</a>` +
		` — the link smartener.` +
		`</td></tr>` +
		`</table></td></tr></table></body></html>`
}
