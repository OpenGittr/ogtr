// Deployment extension point — a deployment may overwrite this module at
// image build time to register additional routes/navigation.
//
// The stock build ships this empty stub, so the app behaves as if the seam
// did not exist. A deployment's replacement module lives in this directory
// and may import shared app modules via relative paths (../components/ui,
// ../lib/api, ../auth/AuthContext, ...).

import type { JSX } from "react";

/** Extra routes rendered inside the authenticated dashboard shell. */
export const extraRoutes: { path: string; element: () => JSX.Element }[] = [];

/** Extra sidebar navigation items, rendered after the built-in ones. */
export const extraNavItems: { to: string; label: string; icon?: () => JSX.Element }[] = [];

/**
 * Optional trailing link rendered on LIMIT_REACHED notices, pointing users
 * at a deployment-specific page that explains the deployment's limits.
 */
export const limitNoticeAction: { to: string; label: string } | null = null;
