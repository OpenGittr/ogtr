// Domain types mirroring backend/models and the auth service results.
// Field names match the backend JSON exactly (snake_case).

export interface User {
  id: number;
  name: string;
  email: string;
  status: string;
  created_at: string;
}

export interface Org {
  id: number;
  name: string;
  slug: string;
  auto_join_domain: string | null;
  created_at: string;
}

/** One org the current user belongs to, with their role in it. */
export interface OrgMembership {
  org_id: number;
  name: string;
  slug: string;
  role: Role;
}

/** One user inside an org, as listed to other org members. */
export interface Member {
  user_id: number;
  name: string;
  email: string;
  role: Role;
  joined_at: string;
}

export interface Invite {
  id: number;
  org_id: number;
  email: string;
  invited_by: number;
  status: "PENDING" | "ACCEPTED" | "REVOKED";
  created_at: string;
}

export type Role = "OWNER" | "MEMBER";

export interface TokenPair {
  access_token: string;
  refresh_token: string;
}

/**
 * GET /api/v1/auth/providers response: which sign-in methods this deployment
 * offers ("google", "microsoft", "dev"). The client IDs are empty unless the
 * matching provider is enabled; a set google_client_id takes precedence over
 * the VITE_GOOGLE_CLIENT_ID build-time fallback.
 */
export interface AuthProvidersInfo {
  providers: string[] | null;
  google_client_id: string;
  microsoft_client_id: string;
}

/** POST /api/v1/auth/{google,microsoft} and /auth/dev response. orgs: [] + active_org_id: 0 = "no org yet". */
export interface AuthResult extends TokenPair {
  user: User;
  orgs: OrgMembership[] | null;
  active_org_id: number;
}

/** GET /api/v1/me response. role is the active-org role ("" / absent when org-less). */
export interface MeResult {
  user: User;
  orgs: OrgMembership[] | null;
  active_org_id: number;
  role?: Role;
}

/** POST /api/v1/orgs response: the org plus a token pair already scoped to it. */
export interface CreateOrgResult extends TokenPair {
  org: Org;
}

export type LinkType = "PUBLIC" | "PRIVATE";

/**
 * A short link (backend models.Link). short_url is computed by the server.
 * user_id is null for links created via a developer API key (api_key_id set).
 */
export interface ShortURL {
  id: number;
  org_id: number;
  user_id: number | null;
  api_key_id?: number | null;
  code: string;
  destination_url: string;
  type: LinkType;
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
  deeplink_config: DeeplinkConfig | null;
  visits: number;
  last_visit_at: string | null;
  created_at: string;
  short_url: string;
}

/** Owner-set mobile deep-link config (links.deeplink_config). */
export interface DeeplinkConfig {
  android?: AndroidDeeplink | null;
  ios?: IOSDeeplink | null;
}

export interface AndroidDeeplink {
  intent: string;
  package: string;
  scheme: string;
  fallback_url: string;
}

export interface IOSDeeplink {
  intent: string;
}

export type RuleConditionType = "is" | "is_not";

/** One is/is_not condition over a value list (case-insensitive match). */
export interface RuleCondition {
  type: RuleConditionType;
  values: string[];
}

/**
 * A target rule (backend models.Rule). device_type compares the visitor's
 * mobile OS; location compares the GeoIP city. Rules evaluate in creation
 * order; the first rule whose present conditions ALL match wins.
 */
export interface TargetRule {
  id: number;
  org_id: number;
  link_id: number;
  target_name: string;
  device_type?: RuleCondition | null;
  location?: RuleCondition | null;
  url: string;
  created_at: string;
}

/** POST /links/{id}/rules and PUT /rules/{ruleId} payload item. */
export interface RuleInput {
  target_name: string;
  device_type?: RuleCondition | null;
  location?: RuleCondition | null;
  url: string;
}

/** GET /api/v1/links response: one page (10/page). */
export interface LinkPage {
  links: ShortURL[];
  page: number;
  per_page: number;
  total: number;
}

// ---------------------------------------------------------------------------
// Analytics (backend models/stats.go — FEATURES.md §5, §6.3)
// ---------------------------------------------------------------------------

/** One point of the clicks-per-day time series. */
export interface DayCount {
  date: string; // YYYY-MM-DD
  clicks: number;
}

/** One per-day bucket of a dimension breakdown. */
export interface DayDimCount {
  date: string; // YYYY-MM-DD
  value: string;
  clicks: number;
}

/** One whole-range bucket of a dimension breakdown. */
export interface DimCount {
  value: string;
  clicks: number;
}

/**
 * Per-day location breakdown at its three levels. Clicks without geo data
 * (GeoIP off, unknown IP) bucket as "Unknown".
 */
export interface LocationPerDay {
  countries: DayDimCount[];
  regions: DayDimCount[];
  cities: DayDimCount[];
}

/** Whole-range location breakdown at its three levels ("Unknown" = no geo). */
export interface LocationTotals {
  countries: DimCount[];
  regions: DimCount[];
  cities: DimCount[];
}

export interface PerDayBreakdowns {
  browser: DayDimCount[];
  device_type: DayDimCount[];
  referrer: DayDimCount[];
  mobile_os: DayDimCount[];
  location: LocationPerDay;
}

export interface TotalBreakdowns {
  browser: DimCount[];
  device_type: DimCount[];
  referrer: DimCount[];
  mobile_os: DimCount[];
  location: LocationTotals;
}

/**
 * deeplink_clicks counts clicks with is_deeplink equal to the request's
 * ?deeplink= flag; mobile_app_opens always counts actual app-intent serves.
 */
export interface DeeplinkStats {
  deeplink_clicks: number;
  mobile_app_opens: number;
}

/** Target-rule effectiveness; null in the report when the link has no rules. */
export interface TargetRuleStats {
  total_clicks: number;
  target_matched: number;
}

/** One row of the report's detailed click list. */
export interface ClickDetail {
  id: number;
  link_id: number;
  custom_tag_id: string | null;
  ts: string;
}

/** GET /api/v1/links/{id}/stats response — the full per-link report. */
export interface LinkStatsReport {
  from: string;
  to: string;
  total_clicks: number;
  clicks_per_day: DayCount[];
  per_day_breakdowns: PerDayBreakdowns;
  total_breakdowns: TotalBreakdowns;
  deeplink: DeeplinkStats;
  target_rule: TargetRuleStats | null;
  clicks: ClickDetail[];
}

/** GET /api/v1/stats/unique-clicks response. */
export interface UniqueClicksResult {
  unique_clicks: number;
}

/** One row of a UTM analysis: a UTM value on one link. */
export interface UTMCount {
  utm_value: string;
  link_id: number;
  code: string;
  url: string;
  clicks: number;
}

/** GET /api/v1/stats/utm response: three parallel analyses. */
export interface UTMAnalysis {
  source_analysis: UTMCount[];
  medium_analysis: UTMCount[];
  campaign_analysis: UTMCount[];
}

// ---------------------------------------------------------------------------
// Custom domains (backend models.Domain — FEATURES.md §1.6)
// ---------------------------------------------------------------------------

export type DomainStatus = "PENDING" | "VERIFIED" | "DISABLED";

/**
 * An org-owned custom short domain. txt_record_name/value are the DNS TXT
 * record to publish for ownership verification; hostnames are globally
 * unique across the deployment. Short URLs display under the primary
 * VERIFIED domain (links keep resolving on the deployment domain too).
 */
export interface OrgDomain {
  id: number;
  org_id: number;
  hostname: string;
  status: DomainStatus;
  verified_at: string | null;
  is_primary: boolean;
  created_at: string;
  txt_record_name: string;
  txt_record_value: string;
}

// ---------------------------------------------------------------------------
// Developer API keys (backend models.APIKey — FEATURES.md §8)
// ---------------------------------------------------------------------------

/**
 * A developer API key row. key_hint is the recognizable prefix ("slk_" +
 * first 8 chars); the full key is only ever available in the create response.
 */
export interface ApiKey {
  id: number;
  org_id: number;
  name: string;
  key_hint: string;
  status: "ENABLED" | "DISABLED";
  created_at: string;
  last_used_at: string | null;
}

/** POST /api/v1/api-keys response: key is the plaintext, shown exactly once. */
export interface CreatedApiKey extends ApiKey {
  key: string;
}

/** PATCH /api/v1/links/{id} payload — destination editing. */
export interface LinkEditInput {
  url: string;
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
}

/** POST /api/v1/links payload. */
export interface ShortenInput {
  url: string;
  type?: LinkType;
  utm_source?: string;
  utm_medium?: string;
  utm_campaign?: string;
}
