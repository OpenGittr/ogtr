// Per-route document titles: every page calls usePageTitle("Links") etc. so
// the browser tab always names the current screen.

import { useEffect } from "react";

const SUFFIX = "ogtr";

export function usePageTitle(title: string): void {
  useEffect(() => {
    document.title = title ? `${title} — ${SUFFIX}` : SUFFIX;
  }, [title]);
}
