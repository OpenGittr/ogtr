# Smoke tests — fresh deployment

Manual checklist for a freshly deployed environment (or a fresh local stack).
Run in order — later steps reuse earlier state. `APP` = the dashboard origin
(e.g. https://app.example.com), `SHORT` = the short domain (e.g. https://links.example.com).
Each step is one action → one expected result.

1. **Health** — `curl SHORT/.well-known/health` → 200, `"status":"UP"` with MySQL `UP`.
2. **Sign in** — open APP, click "Sign in with Google", complete OAuth → lands in the app (no-org onboarding screen on a fresh DB).
2a. **Microsoft sign-in (where enabled)** — only where `AUTH_PROVIDERS` includes `microsoft` (and `MICROSOFT_CLIENT_ID` is set): the login page shows "Sign in with Microsoft"; complete the Microsoft consent (a work/school AND a personal account both work) → lands in the app. Where microsoft is NOT enabled: no Microsoft button, `curl SHORT/api/v1/auth/providers` has no `microsoft` in `providers` and an empty `microsoft_client_id`, and `curl -i -X POST SHORT/api/v1/auth/microsoft -H 'Content-Type: application/json' -d '{"id_token":"x"}'` → 404.
3. **Dev sign-in (dev-mode only)** — only where `AUTH_PROVIDERS` includes `dev` (local evaluation stacks; never production): the login page shows the amber "Development mode sign-in" form; submit any name/email → lands in the app. Where dev is NOT enabled: the form is absent and `curl -i -X POST SHORT/api/v1/auth/dev -H 'Content-Type: application/json' -d '{"email":"x@example.com","name":"X"}'` → 404.
4. **Create org** — enter an org name on onboarding → org created, dashboard loads with the org name in the header.
5. **Shorten** — paste `example.com/hello?x=1` in the shorten form → the creation success card appears: short URL + copy, QR code rendered immediately with a working "Download PNG", and quick actions ("Add custom alias" opens the alias modal, "Set up targeting" opens the targeting drawer, "View analytics" goes to the detail page); destination shows `https://` prefixed; the card is dismissible and is replaced by the next shorten.
6. **Dedupe** — shorten the exact same URL again → the same short code comes back, no duplicate row in the links list.
7. **Redirect + click** — open the short URL in a fresh/incognito tab → 302 to the destination (with auto-UTM params appended); link's visit count increments and last-visit updates on the dashboard.
8. **Unknown code** — `curl -i SHORT/nosuchcode` → 404.
9. **Alias** — links list row ⋯ menu → "Change alias" → set alias `smoke-test` in the modal → short URL becomes `SHORT/smoke-test` and redirects; the list refreshes when the modal closes; setting the same alias on another link → 409 error shown; alias `api` → validation error (reserved). The same modal opens from the link-detail header menu and its "alias set" chip.
10. **QR** — row ⋯ menu → "View QR code" → the modal shows the QR; download it, scan with a phone → resolves to the short URL and redirects.
11. **Deep link** — row ⋯ menu → "Manage targeting" (or the detail header) opens the targeting drawer; configure Android deeplink (intent/package/scheme + fallback); visit with an Android UA (browser device emulation) → intent URI served; desktop visit → normal destination; the detail header now shows a clickable "deep links" chip.
12. **Target rule** — in the targeting drawer add rule "iOS → apple.com"; visit with an iPhone UA → 302 to apple.com; desktop visit → default destination; both clicks appear in analytics with `target_matched` reflected in the target-rule tile; the detail header shows an "N rules" chip that reopens the drawer.
12a. **Edit destination** — row ⋯ menu → "Edit destination" → modal prefilled with the current destination; change the URL, save → the list shows the new destination and `curl -i SHORT/<code>` 302s to the NEW URL; a `link_edits` audit row exists for the change (DB spot-check on self-hosted stacks). A second MEMBER (non-creator) saving in the same modal → friendly 403 message; an org OWNER succeeds.
12b. **Overlay behavior** — any modal/drawer closes on Esc and backdrop click and returns focus to the control that opened it; on a phone-sized window the targeting drawer is a full-screen sheet and no page scrolls horizontally.
13. **Analytics numbers** — open the link's analytics → total clicks matches the visits made above; per-day chart, device/browser/referrer breakdowns and recent-clicks list are populated and consistent.
14. **Location breakdown (GeoIP deployments)** — with `GEOIP_DB_PATH` mounted, `curl SHORT/<code> -H 'X-Forwarded-For: <ip the mmdb resolves>'` plus one unresolvable IP (`1.1.1.1`) → the analytics Breakdown's Location tab shows the resolved place at each of its Country / Region / City levels plus an `Unknown` bucket for the unresolvable click; the per-day toggle shows the same buckets by day. Without GeoIP configured every click is `Unknown` and the tab shows the "No location data — GeoIP not configured?" hint instead.
15. **UTM page** — open the org Analytics page → UTM source/medium/campaign tables show the auto-tagged clicks (source = `UTM_SELF_SOURCE` if configured, else the deployment's `SHORT_DOMAIN`; medium `referrer by Desktop/Mobile`).
15a. **Bare-domain bounce** — `curl -i SHORT/` (no code) → 302 to the configured `WEBSITE_URL`; with `WEBSITE_URL` unset it returns 404. No click is recorded either way.
16. **API key create + use** — create a key on the API-keys page (plaintext shown once) → `curl -X POST SHORT/api/v1/links -H "X-API-Key: <key>" -d '{"url":"example.com/api-smoke"}'` returns 201; the new link shows a "via API" badge in the dashboard.
17. **API key disable** — disable the key in the UI → the same curl now returns 401; the key row shows disabled.
18. **Org isolation spot-check** — sign in as a second user, create a second org, shorten a URL → org A's links/analytics/keys are not visible in org B (and fetching an org-A link ID by API returns 404).
19. **Private link** — as user A create a PRIVATE link; user B (same org) doesn't see it in the list and gets 404 on its detail; the short URL still redirects publicly.
20. **Metrics** — scrape the server metrics port (`:2121/metrics` in-cluster) → Prometheus metrics include request counts for the calls above.
21. **Custom domain — register** — Domains page (owner): add `links.<a-domain-you-control>` → row appears as `pending` with TXT instructions (record name `_ogtr-verify.<hostname>` + value, both with working copy buttons). Adding the deployment's own `SHORT_DOMAIN` (or a subdomain) → validation error; a second org adding the same hostname → 409 "already registered".
22. **Custom domain — verify** — click Verify before publishing the TXT record → inline failure message ("could not be found yet…"); publish the TXT record at your DNS provider, wait for propagation, Verify again → `verified` badge with date. (No-DNS stacks: see LOCAL_DEVELOPMENT.md for the dev-DB flip.)
23. **Custom domain — routing** — `curl -i -H 'Host: <hostname>' SHORT/<own-org-code>` → 302; the same with another org's code → 404; `curl -i -H 'Host: <hostname>' SHORT/` (bare root) → 404, no WEBSITE_URL bounce; an unregistered hostname pointing at the deployment → 404; plain `SHORT/<any-code>` still 302 (global namespace unchanged).
24. **Custom domain — primary display** — click "Set primary" on the verified domain → links list, link detail, the next shorten's success card and the QR code all show `https://<hostname>/<code>`; the QR scan resolves through the custom domain. A MEMBER sees the Domains list read-only (no add/verify/remove controls).
25. **Custom domain — remove** — Remove (inline confirm) the domain → links display reverts to `SHORT/<code>`; the old codes still redirect on SHORT; `curl -H 'Host: <hostname>'` now 404s.

26. **Flagged destination refused** — paste `http://192.168.1.1/admin` (private IP — always
    blocked, no feed config needed) into the shorten form → inline error "This destination was
    flagged by security checks and can't be shortened." including the `ABUSE_CONTACT` line when
    configured; same via curl → 422. Also refused: `https://bit.ly/anything` (shortener
    chaining) and `https://google.com@evil.example/` (userinfo trick). A normal URL still
    shortens fine.
27. **Blocklist feed hit (feed-configured deployments)** — with `BLOCKLIST_FEED_URLS` set,
    shorten a URL whose host is on a configured feed → 422 with the same coarse message; the
    boot logs show each feed loaded ("blocklist feed … loaded: N hosts, M urls").
28. **Edit-to-flagged refused** — edit an existing link's destination to a blocked URL (e.g. a
    private IP) → same 422; the link keeps its previous destination.
29. **Link preview** — append `+` to any short URL (`SHORT/<code>+`) in a browser → org-neutral
    preview page shows the destination as text, a plain "Proceed" link and the "Report this
    link" form; the link's visit count does NOT increase. Renders sensibly on a phone-sized
    window. Unknown code → HTML "No such link" 404.
30. **Abuse report** — submit the preview page's report form (or
    `curl -X POST SHORT/api/v1/report -d '{"code":"<code>","reason":"test"}'`) → success
    notice / 201, and a row appears in `abuse_reports` (DB spot-check). The 6th report from
    the same IP within a minute → 429. A >140-char reason → 422; an unknown code → 404.
31. **Disabled link (410)** — set a test link's status to `DISABLED_ABUSE` (operator SQL, or
    let the re-scan flag it on a stack whose feed fixture lists the destination) →
    `curl -i SHORT/<code>` answers **410** with the "This link has been disabled" HTML page
    (incl. abuse contact); `SHORT/api/v1/resolve?code=<code>` answers 410 JSON; the preview
    page shows the disabled notice. `UPDATE links SET status='ACTIVE' …` restores the 302.
32. **Creation rate limit** — script `LINK_CREATE_RATE`+1 rapid creates with one token → the
    last answers 429 "creating or editing links too quickly"; a minute later creation works
    again.
33. **Resolution guess throttle** — script 61 requests to distinct unknown codes from one IP
    within a minute → 429 on subsequent resolutions from that IP; a different IP (or the same
    one after the cooldown) gets normal 404s/302s.
34. **Reserved aliases** — alias `pricing` (or `login`, `terms`) on an org WITHOUT a custom
    domain → 422 "this alias is reserved"; the same alias on an org with a VERIFIED custom
    domain → accepted; alias `api` → 422 for BOTH orgs (functional category binds everywhere);
    any word from `RESERVED_ALIASES` → 422 for both.

Changes to this checklist should be agreed before editing (project convention).
