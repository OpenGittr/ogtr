// Route table + auth gating. Anonymous users land on /login; authed users
// without an org are funneled to /onboarding; everything else renders inside
// the dashboard shell.

import { Navigate, Route, Routes, useLocation } from "react-router-dom";
import type { ReactNode } from "react";

import { useAuth } from "./auth/AuthContext";
import AppShell from "./components/AppShell";
import { FullPageSpinner } from "./components/ui";
import { extraRoutes } from "./ext";
import AnalyticsPage from "./pages/AnalyticsPage";
import ApiKeysPage from "./pages/ApiKeysPage";
import DomainsPage from "./pages/DomainsPage";
import HomePage from "./pages/HomePage";
import LinkDetailPage from "./pages/LinkDetailPage";
import LinksPage from "./pages/LinksPage";
import LoginPage from "./pages/LoginPage";
import MembersPage from "./pages/MembersPage";
import NotFoundPage from "./pages/NotFoundPage";
import OnboardingPage from "./pages/OnboardingPage";

function RequireAuth({
  children,
  allowOrgless = false,
}: {
  children: ReactNode;
  allowOrgless?: boolean;
}) {
  const { status, activeOrgId } = useAuth();
  const location = useLocation();

  if (status === "loading") return <FullPageSpinner />;

  if (status === "anonymous") {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />;
  }

  if (!allowOrgless && activeOrgId === 0) return <Navigate to="/onboarding" replace />;

  return <>{children}</>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />

      <Route
        path="/onboarding"
        element={
          <RequireAuth allowOrgless>
            <OnboardingPage />
          </RequireAuth>
        }
      />

      <Route
        element={
          <RequireAuth>
            <AppShell />
          </RequireAuth>
        }
      >
        <Route index element={<HomePage />} />
        <Route path="members" element={<MembersPage />} />
        <Route path="domains" element={<DomainsPage />} />
        <Route path="links" element={<LinksPage />} />
        <Route path="links/:id" element={<LinkDetailPage />} />
        <Route path="analytics" element={<AnalyticsPage />} />
        <Route path="api-keys" element={<ApiKeysPage />} />
        {extraRoutes.map((route) => (
          <Route key={route.path} path={route.path} element={<route.element />} />
        ))}
      </Route>

      <Route path="*" element={<NotFoundPage />} />
    </Routes>
  );
}
