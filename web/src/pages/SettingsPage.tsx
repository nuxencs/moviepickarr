import { AccountPage } from "@/components/moviepickarr/account/AccountPage";
import { Shell } from "@/components/moviepickarr/AppShell";

/** Route component for /settings. Lazy-loaded (see StatsPage for the why). */
export function SettingsPage() {
  return (
    <Shell>
      <AccountPage />
    </Shell>
  );
}
