import { Navigate, Outlet, createBrowserRouter, RouterProvider } from "react-router";
import { Layout } from "./components/Layout";
import { Alert, Button, PageLoader } from "./components/ui";
import { useI18n } from "./lib/i18n";
import { useMe, useSettings, useSetupStatus, useSyncPreferences } from "./lib/session";
import { LoginPage, RegisterPage } from "./pages/Auth";
import { ClientsPage } from "./pages/Clients";
import { DashboardPage } from "./pages/Dashboard";
import { IsosPage } from "./pages/Isos";
import { NotFoundPage } from "./pages/NotFound";
import { OnboardingPage } from "./pages/Onboarding";
import { SettingsPage } from "./pages/Settings";

type Stage = "loading" | "error" | "register" | "login" | "onboarding" | "app";

function useStage(): { stage: Stage; retry: () => void } {
  const setup = useSetupStatus();
  const me = useMe();
  const settings = useSettings(!!me.data);
  useSyncPreferences(settings.data);
  const retry = () => {
    setup.refetch();
    me.refetch();
  };
  if (setup.isError || me.isError) return { stage: "error", retry };
  if (setup.isLoading || me.isLoading) return { stage: "loading", retry };
  if (setup.data?.needsRegistration) return { stage: "register", retry };
  if (!me.data) return { stage: "login", retry };
  if (!setup.data?.setupComplete) return { stage: "onboarding", retry };
  return { stage: "app", retry };
}

const home: Record<Stage, string> = {
  loading: "/",
  error: "/",
  register: "/register",
  login: "/login",
  onboarding: "/onboarding",
  app: "/",
};

/** Routes are only reachable in their stage; everything else redirects. */
function Gate({ stage: allowed }: { stage: Stage }) {
  const { t } = useI18n();
  const { stage, retry } = useStage();
  if (stage === "loading") return <PageLoader />;
  if (stage === "error")
    return (
      <div className="mx-auto max-w-md p-8">
        <Alert tone="danger" title={t("common.error")}>
          <Button size="sm" variant="secondary" className="mt-2" onClick={retry}>
            {t("common.retry")}
          </Button>
        </Alert>
      </div>
    );
  if (stage !== allowed) return <Navigate to={home[stage]} replace />;
  return <Outlet />;
}

const router = createBrowserRouter([
  { element: <Gate stage="register" />, children: [{ path: "/register", element: <RegisterPage /> }] },
  { element: <Gate stage="login" />, children: [{ path: "/login", element: <LoginPage /> }] },
  { element: <Gate stage="onboarding" />, children: [{ path: "/onboarding", element: <OnboardingPage /> }] },
  {
    element: <Gate stage="app" />,
    children: [
      {
        element: <Layout />,
        children: [
          { path: "/", element: <DashboardPage /> },
          { path: "/isos", element: <IsosPage /> },
          { path: "/clients", element: <ClientsPage /> },
          { path: "/settings", element: <SettingsPage /> },
          { path: "*", element: <NotFoundPage /> },
        ],
      },
    ],
  },
]);

export default function App() {
  return <RouterProvider router={router} />;
}
